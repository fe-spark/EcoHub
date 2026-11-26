package collect

import (
	"fmt"
	"log"
	"runtime/debug"
	"server/internal/sourcecatalog"
	"sort"
	"sync"
	"time"

	filmcache "server/internal/film/cache"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/film/writer"
	"server/internal/infra/syslog"
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
	streamHooksMu         sync.RWMutex
	publishStreamWindowFn = publishStreamWindowReal
	preferredSourceIDFn   = lookupPreferredSourceID
	streamNowFn           = time.Now
)

func currentStreamNow() time.Time {
	streamHooksMu.RLock()
	fn := streamNowFn
	streamHooksMu.RUnlock()
	return fn()
}

func currentPreferredSourceID() string {
	streamHooksMu.RLock()
	fn := preferredSourceIDFn
	streamHooksMu.RUnlock()
	return fn()
}

func callPublishStreamWindow(mids []int64) error {
	streamHooksMu.RLock()
	fn := publishStreamWindowFn
	streamHooksMu.RUnlock()
	return fn(mids)
}

func newStreamPublishBuffer() *streamPublishBuffer {
	now := time.Now()
	return &streamPublishBuffer{
		pending:    make(map[int64]bool),
		lastPref:   now,
		lastBackup: now,
	}
}

func (b *streamPublishBuffer) reset(now time.Time) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending = make(map[int64]bool)
	b.lastPref = now
	b.lastBackup = now
}

func lookupPreferredSourceID() string {
	src := sourcecatalog.CurrentPrimarySource()
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
	return currentPreferredSourceID() == id
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
				func() {
					defer func() {
						if r := recover(); r != nil {
							syslog.Errorf("[Spider][StreamPublish] 定时刷窗 panic: %v\n%s", r, debug.Stack())
						}
					}()
					if err := drainStreamPublish(false); err != nil {
						log.Printf("[Spider][StreamPublish] 定时刷窗失败: %v", err)
					}
				}()
			}
		}()
	})
}

func drainStreamPublish(force bool) error {
	_, err := drainStreamPublishLocked(force, true)
	return err
}

func drainStreamPublishLocked(force, acquire bool) (int, error) {
	totalDrained := 0
	for {
		mids, flags := streamBuf.take(currentStreamNow(), force)
		if len(mids) == 0 {
			if !force && streamBuf.pendingCount() >= streamBackpressureN {
				mids, flags = streamBuf.take(currentStreamNow(), true)
			}
			if len(mids) == 0 {
				return totalDrained, nil
			}
		}
		err := publishStreamWindowGuarded(mids, acquire)
		if err != nil {
			streamBuf.putBack(flags)
			return totalDrained, err
		}
		totalDrained += len(mids)
		if !force && streamBuf.pendingCount() < streamBackpressureN {
			return totalDrained, nil
		}
		if force && streamBuf.pendingCount() == 0 {
			return totalDrained, nil
		}
	}
}

// publishStreamWindowGuarded 发布一窗：锁用 defer 释放，panic 转成错误，避免锁和发布计数泄漏。
func publishStreamWindowGuarded(mids []int64, acquire bool) (err error) {
	if acquire {
		collectLifecycle.beginPublish()
		publishMu.Lock()
		defer func() {
			publishMu.Unlock()
			collectLifecycle.endPublish()
		}()
	}
	defer func() {
		if r := recover(); r != nil {
			syslog.Errorf("[Spider][StreamPublish] 发布窗口 panic: %v\n%s", r, debug.Stack())
			err = fmt.Errorf("发布窗口 panic: %v", r)
		}
	}()
	return callPublishStreamWindow(mids)
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
	// 标签跟着本窗已入库影片走。失败只记日志，不挡住列表可见。
	if err := writer.UpsertSearchTagsByMids(mids...); err != nil {
		log.Printf("[Spider][StreamPublish] 搜索标签增量失败 mids=%d err=%v", len(mids), err)
	}
	_, err := publishFilmSnapshot(mids)
	return err
}

func finalizeStreamPublish(takeMIDs func() []int64, warmupHome ...bool) error {
	if _, err := drainStreamPublishLocked(true, false); err != nil {
		return err
	}
	var mids []int64
	if takeMIDs != nil {
		mids = takeMIDs()
	}
	schedulePrimarySearchReseal(mids)
	filmcache.ClearTVBoxConfigCache()
	shouldWarmup := len(warmupHome) == 0 || warmupHome[0]
	if shouldWarmup {
		filmsnapshot.FlushSnapshotCacheInvalidation()
	}
	return nil
}

func resetStreamPublishBufferForTest() {
	// 等待后台定时刷窗的当前发布窗口结束后，在锁内原位清空缓冲。
	publishMu.Lock()
	defer publishMu.Unlock()
	streamBuf.reset(currentStreamNow())
}

// setStreamPublishHooksForTest 在发布锁与钩子锁保护下替换流式发布测试桩，并返回恢复函数。
func setStreamPublishHooksForTest(pub func([]int64) error, pref func() string) func() {
	publishMu.Lock()
	streamHooksMu.Lock()
	origPub := publishStreamWindowFn
	origPref := preferredSourceIDFn
	if pub != nil {
		publishStreamWindowFn = pub
	}
	if pref != nil {
		preferredSourceIDFn = pref
	}
	streamBuf.reset(streamNowFn())
	streamHooksMu.Unlock()
	publishMu.Unlock()

	return func() {
		publishMu.Lock()
		streamHooksMu.Lock()
		streamBuf.reset(streamNowFn())
		publishStreamWindowFn = origPub
		preferredSourceIDFn = origPref
		streamHooksMu.Unlock()
		publishMu.Unlock()
	}
}
