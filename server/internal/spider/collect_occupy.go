package spider

import (
	"log"
	"strings"
	"sync"
	"sync/atomic"

	"server/internal/model"
	"server/internal/spider/progress"
)

var (
	// collectingSources 正采集队列：sourceID -> occupy token。
	// 新队列出发前先看这里，已在采的源直接跳过，两条队列互不等待。
	collectingSourcesMu sync.Mutex
	collectingSources   = make(map[string]uint64)
	occupySeq           atomic.Uint64
)

func collectSourceID(id string) string {
	return strings.TrimSpace(id)
}

func occupyCollectSources(sources []model.FilmSource, tag string) []model.FilmSource {
	if len(sources) == 0 {
		return sources
	}
	occupied := make([]model.FilmSource, 0, len(sources))
	seen := make(map[string]struct{}, len(sources))
	candidates := make([]model.FilmSource, 0, len(sources))

	collectingSourcesMu.Lock()
	for _, source := range sources {
		id := collectSourceID(source.Id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			log.Printf("[%s] 站点 %s 在本轮采集列表中重复，跳过", tag, source.Name)
			continue
		}
		seen[id] = struct{}{}
		if _, busy := collectingSources[id]; busy {
			log.Printf("[%s] 站点 %s 已在正采集队列中，跳过", tag, source.Name)
			continue
		}
		candidates = append(candidates, source)
	}
	collectingSourcesMu.Unlock()

	for _, source := range candidates {
		id := collectSourceID(source.Id)
		if progress.IsAlreadyQueuedOrRunning(id) {
			log.Printf("[%s] 站点 %s 已在采集队列或正在运行，跳过", tag, source.Name)
			continue
		}
		collectingSourcesMu.Lock()
		if _, busy := collectingSources[id]; busy {
			collectingSourcesMu.Unlock()
			log.Printf("[%s] 站点 %s 已在正采集队列中，跳过", tag, source.Name)
			continue
		}
		collectingSources[id] = occupySeq.Add(1)
		collectingSourcesMu.Unlock()
		occupied = append(occupied, source)
	}
	return occupied
}

func releaseCollectSources(sources []model.FilmSource) {
	if len(sources) == 0 {
		return
	}
	collectingSourcesMu.Lock()
	defer collectingSourcesMu.Unlock()
	for _, source := range sources {
		id := collectSourceID(source.Id)
		if id == "" {
			continue
		}
		delete(collectingSources, id)
	}
}

func releaseCollectSourceIDs(ids ...string) {
	if len(ids) == 0 {
		return
	}
	collectingSourcesMu.Lock()
	defer collectingSourcesMu.Unlock()
	for _, id := range ids {
		id = collectSourceID(id)
		if id == "" {
			continue
		}
		delete(collectingSources, id)
	}
}

func snapshotOccupyTokens(sources []model.FilmSource) map[string]uint64 {
	tokens := make(map[string]uint64, len(sources))
	collectingSourcesMu.Lock()
	defer collectingSourcesMu.Unlock()
	for _, source := range sources {
		id := collectSourceID(source.Id)
		if id == "" {
			continue
		}
		if tok, ok := collectingSources[id]; ok {
			tokens[id] = tok
		}
	}
	return tokens
}

func releaseCollectTokens(tokens map[string]uint64) {
	if len(tokens) == 0 {
		return
	}
	collectingSourcesMu.Lock()
	defer collectingSourcesMu.Unlock()
	for id, tok := range tokens {
		id = collectSourceID(id)
		if id == "" {
			continue
		}
		if collectingSources[id] == tok {
			delete(collectingSources, id)
		}
	}
}

func isOccupiedCollectSource(sourceID string) bool {
	sourceID = collectSourceID(sourceID)
	if sourceID == "" {
		return false
	}
	collectingSourcesMu.Lock()
	_, ok := collectingSources[sourceID]
	collectingSourcesMu.Unlock()
	if !ok {
		return false
	}
	// 先放开占用锁再读进度。读进度的人可能正在等这把占用锁，反过来拿会卡住。
	snap, hasSnap := progress.Snapshot(sourceID)
	if hasSnap && progress.IsTerminalStatus(snap.Status) && !progress.IsTaskRunning(sourceID) {
		collectingSourcesMu.Lock()
		delete(collectingSources, sourceID)
		collectingSourcesMu.Unlock()
		log.Printf("[Spider] 占用释放 source=%s status=%s 原因=已终态且无任务", sourceID, snap.Status)
		return false
	}
	return true
}

// releaseQueuedCollectOccupies 放掉还没跑起来的队列占用。运行中的任务仍由自己的退出释放。
func releaseQueuedCollectOccupies() {
	live := make(map[string]struct{})
	progress.RangeTasks(func(id, _ string) bool {
		live[id] = struct{}{}
		return true
	})
	collectingSourcesMu.Lock()
	defer collectingSourcesMu.Unlock()
	for id := range collectingSources {
		if _, ok := live[id]; ok {
			continue
		}
		delete(collectingSources, id)
	}
}

func abortUnstartedCollect(id string, batchCtx *collectBatchContext) {
	if batchCtx != nil && !batchCtx.ownsOccupy(id) {
		batchCtx.releaseSourceOccupy(id)
		return
	}
	progress.Update(id, func(cur *model.CollectProgress) {
		switch cur.Status {
		case progress.StatusStarting, progress.StatusRunning:
			cur.Status = progress.StatusFailed
		}
	})
	batchCtx.releaseSourceOccupy(id)
}

// abandonQueuedCollectSource 站点不再采集（排队中停止 / 一键终止未派发）：立刻放占用，允许单独重试。
func abandonQueuedCollectSource(source model.FilmSource, batchCtx *collectBatchContext) {
	progress.MarkStopped(source.Id)
	batchCtx.releaseSourceOccupy(source.Id)
}
