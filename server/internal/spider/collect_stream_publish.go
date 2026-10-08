package spider

import (
	"log"
	"sort"
	"sync"
	"time"

	"server/internal/repository"
	filmcache "server/internal/repository/film/cache"
	filmsnapshot "server/internal/repository/film/snapshot"
)

const (
	streamPreferredCount = 300
	streamPreferredWait  = 3 * time.Second
	streamBackupCount    = 1000
	streamBackupWait     = 10 * time.Second
	streamWindowMax      = 1000
	streamBackpressureN  = 5000
)

type streamPublishBuffer struct {
	mu         sync.Mutex
	pending    map[int64]bool
	lastPref   time.Time
	lastBackup time.Time
}

var (
	streamBuf             = newStreamPublishBuffer()
	streamFlushOnce       sync.Once
	publishStreamWindowFn = publishStreamWindowReal
	preferredSourceIDFn   = lookupPreferredSourceID
	streamNowFn           = time.Now
)

func newStreamPublishBuffer() *streamPublishBuffer {
	now := time.Now()
	return &streamPublishBuffer{
		pending:    make(map[int64]bool),
		lastPref:   now,
		lastBackup: now,
	}
}

func lookupPreferredSourceID() string {
	src := repository.GetActiveCollectSource()
	if src == nil {
		return ""
	}
	return src.Id
}

func isPreferredCollectSource(id string) bool {
	id = collectSourceID(id)
	if id == "" {
		return false
	}
	return preferredSourceIDFn() == id
}

func (b *streamPublishBuffer) add(preferred bool, mids []int64) {
	if b == nil || len(mids) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if preferred {
			b.pending[mid] = true
			continue
		}
		if _, exists := b.pending[mid]; !exists {
			b.pending[mid] = false
		}
	}
}

func (b *streamPublishBuffer) pendingCount() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}

func (b *streamPublishBuffer) putBack(flags map[int64]bool) {
	if b == nil || len(flags) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for mid, preferred := range flags {
		if mid <= 0 {
			continue
		}
		if preferred {
			b.pending[mid] = true
			continue
		}
		if _, exists := b.pending[mid]; !exists {
			b.pending[mid] = false
		}
	}
}

func (b *streamPublishBuffer) take(now time.Time, force bool) ([]int64, map[int64]bool) {
	if b == nil {
		return nil, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.pending) == 0 {
		return nil, nil
	}

	prefN, backupN := 0, 0
	for _, preferred := range b.pending {
		if preferred {
			prefN++
		} else {
			backupN++
		}
	}
	prefDue := force || prefN >= streamPreferredCount || (prefN > 0 && now.Sub(b.lastPref) >= streamPreferredWait)
	backupDue := force || backupN >= streamBackupCount || (backupN > 0 && now.Sub(b.lastBackup) >= streamBackupWait)
	pressure := len(b.pending) >= streamBackpressureN
	if !prefDue && !backupDue && !pressure {
		return nil, nil
	}

	takePref := force || prefDue || pressure
	takeBackup := force || backupDue || pressure
	out := make([]int64, 0, streamWindowMax)
	flags := make(map[int64]bool, streamWindowMax)

	if takePref && prefN > 0 {
		picked := takePendingByFlag(b.pending, true, streamWindowMax)
		for _, mid := range picked {
			out = append(out, mid)
			flags[mid] = true
		}
		if len(picked) > 0 {
			b.lastPref = now
		}
	}
	remain := streamWindowMax - len(out)
	if remain > 0 && takeBackup && backupN > 0 {
		picked := takePendingByFlag(b.pending, false, remain)
		for _, mid := range picked {
			out = append(out, mid)
			flags[mid] = false
		}
		if len(picked) > 0 {
			b.lastBackup = now
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, flags
}

func takePendingByFlag(pending map[int64]bool, preferred bool, max int) []int64 {
	if max <= 0 {
		return nil
	}
	out := make([]int64, 0, max)
	for mid, flag := range pending {
		if flag != preferred {
			continue
		}
		delete(pending, mid)
		if mid <= 0 {
			continue
		}
		out = append(out, mid)
		if len(out) >= max {
			break
		}
	}
	return out
}

func ensureStreamFlushLoop() {
	streamFlushOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if err := drainStreamPublish(false); err != nil {
					log.Printf("[Spider][StreamPublish] 定时刷窗失败: %v", err)
				}
			}
		}()
	})
}

func drainStreamPublish(force bool) error {
	return drainStreamPublishLocked(force, true)
}

func drainStreamPublishLocked(force, acquire bool) error {
	for {
		mids, flags := streamBuf.take(streamNowFn(), force)
		if len(mids) == 0 {
			if !force && streamBuf.pendingCount() >= streamBackpressureN {
				mids, flags = streamBuf.take(streamNowFn(), true)
			}
			if len(mids) == 0 {
				return nil
			}
		}
		if acquire {
			collectLifecycle.beginPublish()
			publishMu.Lock()
		}
		err := publishStreamWindowFn(mids)
		if acquire {
			publishMu.Unlock()
			collectLifecycle.endPublish()
		}
		if err != nil {
			streamBuf.putBack(flags)
			return err
		}
		if !force && streamBuf.pendingCount() < streamBackpressureN {
			return nil
		}
		if force && streamBuf.pendingCount() == 0 {
			return nil
		}
	}
}

func enqueueStreamPublish(sourceID string, mids []int64) error {
	if len(mids) == 0 {
		return nil
	}
	streamBuf.add(isPreferredCollectSource(sourceID), mids)
	ensureStreamFlushLoop()
	if err := drainStreamPublish(false); err != nil {
		return err
	}
	for streamBuf.pendingCount() >= streamBackpressureN {
		if err := drainStreamPublish(true); err != nil {
			return err
		}
	}
	return nil
}

func publishStreamWindowReal(mids []int64) error {
	if len(mids) == 0 {
		return nil
	}
	version, err := publishFilmSnapshot(mids)
	if err != nil {
		return err
	}
	if version != "" {
		filmsnapshot.ScheduleSnapshotPublishedNotify(version)
	}
	return nil
}

func finalizeStreamPublish(masterMIDs []int64) error {
	if err := drainStreamPublishLocked(true, false); err != nil {
		return err
	}
	scheduleMasterSearchTagsRefresh(masterMIDs)
	filmcache.ClearTVBoxConfigCache()
	filmsnapshot.FlushSnapshotCacheInvalidation()
	filmsnapshot.FlushSnapshotPublishedNotify()
	return nil
}

func resetStreamPublishBufferForTest() {
	streamBuf = newStreamPublishBuffer()
}
