package film

import (
	"sync"
	"time"

	"server/internal/config"
	"server/internal/model"
)

// collectCacheCoalescer 合并采集热路径中的 Redis 缓存清理。
// 每页 clear 会反复 DEL 分类树/首页缓存；改为攒 pid，按间隔或站点结束统一清理。
type collectCacheCoalescer struct {
	mu          sync.Mutex
	pendingPids map[int64]struct{}
	dirty       bool
	lastFlush   time.Time
	minInterval time.Duration
}

var collectCaches = &collectCacheCoalescer{
	pendingPids: make(map[int64]struct{}),
	minInterval: collectCacheMinInterval(),
}

func collectCacheMinInterval() time.Duration {
	sec := config.CollectCacheFlushIntervalSec
	if sec <= 0 {
		sec = config.DefaultCollectCacheFlushIntervalSec
	}
	return time.Duration(sec) * time.Second
}

// NoteCollectCacheInvalidation 记录采集写库后需要失效的分类 pid。
func NoteCollectCacheInvalidation(pids []int64) {
	if len(pids) == 0 {
		return
	}
	now := time.Now()
	collectCaches.mu.Lock()
	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		collectCaches.pendingPids[pid] = struct{}{}
		collectCaches.dirty = true
	}
	shouldFlush := collectCaches.dirty && (collectCaches.lastFlush.IsZero() || now.Sub(collectCaches.lastFlush) >= collectCaches.minInterval)
	collectCaches.mu.Unlock()
	if shouldFlush {
		FlushCollectCacheInvalidations()
	}
}

// NoteCollectCacheInvalidationByIndexes 从 FilmIndex 提取 pid 并合并清理。
func NoteCollectCacheInvalidationByIndexes(list []model.FilmIndex) {
	if len(list) == 0 {
		return
	}
	pids := make([]int64, 0, len(list))
	for _, item := range list {
		if item.Pid > 0 {
			pids = append(pids, item.Pid)
		}
	}
	NoteCollectCacheInvalidation(pids)
}

// FlushCollectCacheInvalidations 强制清理所有 pending 的采集相关缓存。
func FlushCollectCacheInvalidations() {
	collectCaches.mu.Lock()
	if !collectCaches.dirty && len(collectCaches.pendingPids) == 0 {
		collectCaches.mu.Unlock()
		return
	}
	pidSet := collectCaches.pendingPids
	collectCaches.pendingPids = make(map[int64]struct{})
	collectCaches.dirty = false
	collectCaches.lastFlush = time.Now()
	collectCaches.mu.Unlock()

	clearFilmIndexCachesByPidSet(pidSet)
}
