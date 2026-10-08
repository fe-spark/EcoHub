package spider

import (
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/model"
	"server/internal/notify"
	"server/internal/spider/progress"
)

// publishMu 全局快照发布互斥锁，确保向 MySQL 发布快照时单次只有一个线程在执行
var publishMu sync.Mutex

var (
	activeBatchesMu sync.Mutex
	activeBatches   = make(map[*collectBatchContext]struct{})

	// collectingSources 正采集队列：sourceID -> occupy token。
	// 新队列出发前先看这里，已在采的源直接跳过，两条队列互不等待。
	collectingSourcesMu sync.Mutex
	collectingSources   = make(map[string]uint64)
	occupySeq           atomic.Uint64
)

func registerActiveBatch(b *collectBatchContext) {
	if b == nil {
		return
	}
	activeBatchesMu.Lock()
	defer activeBatchesMu.Unlock()
	activeBatches[b] = struct{}{}
}

func unregisterActiveBatch(b *collectBatchContext) {
	if b == nil {
		return
	}
	activeBatchesMu.Lock()
	defer activeBatchesMu.Unlock()
	delete(activeBatches, b)
}

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
	defer collectingSourcesMu.Unlock()
	_, ok := collectingSources[sourceID]
	return ok
}

// collectBatchContext 批次上下文：封装单次采集运行的全部生命周期与状态（完全自闭环，跨批次零耦合）
type collectBatchContext struct {
	mu                 sync.Mutex
	trigger            string
	tag                string
	sources            []model.FilmSource
	batch              *notify.ChangeBatch
	startedAt          time.Time
	isStandalone       bool
	masterAffectedMIDs map[int64]struct{}
	finishedSources    map[string]model.FilmSource
	occupyTokens       map[string]uint64
}

func newCollectBatchContext(trigger, tag string, sources []model.FilmSource, batch *notify.ChangeBatch, startedAt time.Time, isStandalone ...bool) *collectBatchContext {
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	if batch == nil {
		batch = notify.StartChangeBatch()
	}
	standalone := false
	if len(isStandalone) > 0 {
		standalone = isStandalone[0]
	}
	b := &collectBatchContext{
		trigger:            trigger,
		tag:                tag,
		sources:            sources,
		batch:              batch,
		startedAt:          startedAt,
		isStandalone:       standalone,
		masterAffectedMIDs: make(map[int64]struct{}),
		finishedSources:    make(map[string]model.FilmSource),
		occupyTokens:       snapshotOccupyTokens(sources),
	}
	registerActiveBatch(b)
	return b
}

func (b *collectBatchContext) addAffectedMIDs(s *model.FilmSource, h int, mids []int64) {
	if b == nil || len(mids) == 0 || s == nil {
		return
	}
	b.mu.Lock()
	for _, mid := range mids {
		if mid > 0 {
			b.masterAffectedMIDs[mid] = struct{}{}
		}
	}
	sourceID := s.Id
	b.mu.Unlock()
	if err := enqueueStreamPublish(sourceID, mids); err != nil {
		log.Printf("[Spider][StreamPublish] 流式刷窗失败 source=%s err=%v", sourceID, err)
	}
}

// IsStandalone 实现 fetcher.Batch：单站采集，不与其他站点合并发布。
func (b *collectBatchContext) IsStandalone() bool {
	return b != nil && b.isStandalone
}

// AddAffectedMIDs 实现 fetcher.Batch：记录影响到的全局 mid。
func (b *collectBatchContext) AddAffectedMIDs(s *model.FilmSource, h int, mids []int64) {
	b.addAffectedMIDs(s, h, mids)
}

// NoteCollectedMIDs 实现 fetcher.Batch：累计本源应进更新列表的 mid。
func (b *collectBatchContext) NoteCollectedMIDs(sourceID, sourceName string, mids []int64) {
	if b == nil {
		return
	}
	noteCollectedMIDs(b.batch, sourceID, sourceName, mids)
}

func (b *collectBatchContext) dropOccupyToken(id string) {
	if b == nil {
		return
	}
	id = collectSourceID(id)
	if id == "" {
		return
	}
	b.mu.Lock()
	delete(b.occupyTokens, id)
	b.mu.Unlock()
}

func abortUnstartedCollect(id string, batchCtx *collectBatchContext) {
	progress.Update(id, func(cur *model.CollectProgress) {
		switch cur.Status {
		case progress.StatusStarting, progress.StatusRunning:
			cur.Status = progress.StatusFailed
		}
	})
	if batchCtx != nil {
		batchCtx.dropOccupyToken(id)
	}
	releaseCollectSourceIDs(id)
}

// abandonQueuedCollectSource 站点不再采集（排队中停止 / 一键终止未派发）：记完成并立刻放占用，允许单独重试。
func abandonQueuedCollectSource(source model.FilmSource, batchCtx *collectBatchContext) {
	progress.MarkStopped(source.Id)
	if batchCtx != nil {
		batchCtx.markSourceFinished(source)
		batchCtx.dropOccupyToken(source.Id)
	}
	releaseCollectSourceIDs(source.Id)
}

func (b *collectBatchContext) markSourceFinished(source model.FilmSource) {
	if b == nil {
		return
	}
	source.Id = strings.TrimSpace(source.Id)
	if source.Id == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.finishedSources[source.Id] = source
}

func (b *collectBatchContext) flushAndFinalize() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	if len(b.finishedSources) == 0 && len(b.masterAffectedMIDs) == 0 && streamBuf.pendingCount() == 0 {
		b.mu.Unlock()
		return nil
	}

	masterMIDs := make([]int64, 0, len(b.masterAffectedMIDs))
	for mid := range b.masterAffectedMIDs {
		if mid > 0 {
			masterMIDs = append(masterMIDs, mid)
		}
	}
	sort.Slice(masterMIDs, func(i, j int) bool { return masterMIDs[i] < masterMIDs[j] })
	finishedMap := b.finishedSources
	b.finishedSources = make(map[string]model.FilmSource)
	b.masterAffectedMIDs = make(map[int64]struct{})
	b.mu.Unlock()

	progress.MarkSourcesFinalizing(finishedMap)

	collectLifecycle.beginPublish()
	defer collectLifecycle.endPublish()
	publishMu.Lock()
	defer publishMu.Unlock()

	if err := finalizeStreamPublish(masterMIDs); err != nil {
		progress.MarkSourcesFinalizeFailed(finishedMap)
		return err
	}
	progress.MarkSourcesPublished(finishedMap)
	return nil
}

func (b *collectBatchContext) close() {
	if b == nil {
		return
	}
	unregisterActiveBatch(b)
	b.mu.Lock()
	tokens := b.occupyTokens
	b.occupyTokens = nil
	b.mu.Unlock()
	releaseCollectTokens(tokens)
}

func (b *collectBatchContext) emitSummary(finalizeErr error) {
	if b == nil {
		return
	}
	defer b.close()
	emitBatchSummaryForSources(b.batch, b.trigger, b.sources, b.startedAt, finalizeErr)
}
