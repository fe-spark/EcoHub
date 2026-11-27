package writer

import (
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"

	"server/internal/film/shared"
	"server/internal/model"
	"server/internal/taxonomy"
	"server/internal/utils"
)

const matchKeyLockStripes = 65536

var (
	filmMidLocks      [2048]sync.Mutex
	filmMatchKeyLocks [matchKeyLockStripes]sync.Mutex
	sourceVodLocks    [2048]sync.Mutex
)

func sourceVodLockIndex(sourceID string, sourceMid int64) int {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.TrimSpace(sourceID)))
	_, _ = h.Write([]byte{':'})
	_, _ = h.Write([]byte(strconv.FormatInt(sourceMid, 10)))
	return int(h.Sum64() % uint64(len(sourceVodLocks)))
}

func getFilmMidLock(mid int64) *sync.Mutex {
	if mid <= 0 {
		return &filmMidLocks[0]
	}
	return &filmMidLocks[uint64(mid)%2048]
}

// LockFilmMids 按分片锁下标升序加锁全部指定 mid 对应的分段互斥锁，防止删片与并发写入死锁。
func LockFilmMids(mids []int64) func() {
	if len(mids) == 0 {
		return func() {}
	}
	buckets := make([]int, 0, len(mids))
	seen := make(map[int]struct{}, len(mids))
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		idx := int(uint64(mid) % 2048)
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		buckets = append(buckets, idx)
	}
	if len(buckets) == 0 {
		return func() {}
	}
	sort.Ints(buckets)
	for _, idx := range buckets {
		filmMidLocks[idx].Lock()
	}
	return func() {
		for i := len(buckets) - 1; i >= 0; i-- {
			filmMidLocks[buckets[i]].Unlock()
		}
	}
}

func matchKeyLockIndex(key string) int {
	key = strings.TrimSpace(key)
	if key == "" {
		return 0
	}
	hash := utils.GenerateHashKey(key)
	var idx uint64
	if len(hash) >= 8 {
		idx, _ = strconv.ParseUint(hash[:8], 16, 32)
	}
	return int(idx % matchKeyLockStripes)
}

func matchKeyLockIndexes(keys []string) []int {
	seen := make(map[int]struct{}, len(keys))
	out := make([]int, 0, len(keys))
	for _, key := range keys {
		idx := matchKeyLockIndex(key)
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		out = append(out, idx)
	}
	sort.Ints(out)
	return out
}

func resolveMatchCategoryPid(source *model.FilmSource, detail model.MovieDetail) int64 {
	sourceId := ""
	if source != nil {
		sourceId = strings.TrimSpace(source.Id)
	}
	if sourceId == "" || sourceId == "manual" {
		if detail.Cid > 0 {
			if root := taxonomy.GetRootId(detail.Cid); root > 0 {
				return root
			}
		}
		if detail.Pid > 0 {
			return detail.Pid
		}
		return detail.RawPid
	}

	sourceCid := detail.Cid
	sourcePid := detail.Pid
	if detail.RawCid > 0 {
		sourceCid = detail.RawCid
	}
	if detail.RawPid > 0 {
		sourcePid = detail.RawPid
	}

	if sourceCid > 0 {
		if localCid := taxonomy.GetLocalCategoryId(sourceId, sourceCid); localCid > 0 {
			if root := taxonomy.GetRootId(localCid); root > 0 {
				return root
			}
		}
	}
	if sourcePid > 0 {
		if localPid := taxonomy.GetLocalCategoryId(sourceId, sourcePid); localPid > 0 {
			if root := taxonomy.GetRootId(localPid); root > 0 {
				return root
			}
		}
	}
	if detail.RawPid > 0 {
		return detail.RawPid
	}
	return detail.Pid
}

func peerMatchKeys(source *model.FilmSource, detail model.MovieDetail) []string {
	pid := resolveMatchCategoryPid(source, detail)
	keys := shared.BuildMovieMatchKeysWithCategory(detail.DbId, detail.Name, pid)
	sort.Strings(keys)
	return keys
}

func lockPeerCollect(source *model.FilmSource, detail model.MovieDetail) func() {
	if source == nil || strings.TrimSpace(source.Id) == "" {
		return func() {}
	}
	if detail.Id <= 0 {
		mu := getSourceWriteLock(source.Id)
		mu.Lock()
		return mu.Unlock
	}
	idx := sourceVodLockIndex(source.Id, detail.Id)
	sourceVodLocks[idx].Lock()
	return sourceVodLocks[idx].Unlock
}

// lockPeerCollectPage 按 (source_id, source_mid) 分片哈希升序加锁，同站不同分片页可完全并发落库，避免同站整站串行阻塞。
func lockPeerCollectPage(source *model.FilmSource, details []model.MovieDetail) func() {
	if source == nil || strings.TrimSpace(source.Id) == "" {
		return func() {}
	}
	if len(details) == 0 {
		return func() {}
	}
	seen := make(map[int]struct{}, len(details))
	buckets := make([]int, 0, len(details))
	hasFallback := false
	for _, d := range details {
		if d.Id <= 0 {
			hasFallback = true
			continue
		}
		idx := sourceVodLockIndex(source.Id, d.Id)
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		buckets = append(buckets, idx)
	}
	var srcMu *sync.Mutex
	if hasFallback {
		srcMu = getSourceWriteLock(source.Id)
		srcMu.Lock()
	}
	sort.Ints(buckets)
	for _, idx := range buckets {
		sourceVodLocks[idx].Lock()
	}
	return func() {
		for i := len(buckets) - 1; i >= 0; i-- {
			sourceVodLocks[buckets[i]].Unlock()
		}
		if srcMu != nil {
			srcMu.Unlock()
		}
	}
}

func saveSinglePeerDetailSynced(source *model.FilmSource, detail model.MovieDetail) (int64, bool, bool, error) {
	unlock := lockPeerCollect(source, detail)
	defer unlock()
	mid, isNew, playChanged, _, err := saveSinglePeerDetailTx(source, detail)
	return mid, isNew, playChanged, err
}
