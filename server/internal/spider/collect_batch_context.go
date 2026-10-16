package spider

import (
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/model"
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
	startedAt          time.Time
	isStandalone       bool
	primaryWarmedUp    bool
	masterAffectedMIDs map[int64]struct{}
	notifyMIDs         map[int64]struct{}
	sourceNotifyMIDs   map[string]map[int64]struct{}
	sourceErrors       map[string]string
	finishedSources    map[string]model.FilmSource
	occupyTokens       map[string]uint64
}

func newCollectBatchContext(trigger, tag string, sources []model.FilmSource, startedAt time.Time, isStandalone ...bool) *collectBatchContext {
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	standalone := false
	if len(isStandalone) > 0 {
		standalone = isStandalone[0]
	}
	b := &collectBatchContext{
		trigger:            trigger,
		tag:                tag,
		sources:            sources,
		startedAt:          startedAt,
		isStandalone:       standalone,
		masterAffectedMIDs: make(map[int64]struct{}),
		notifyMIDs:         make(map[int64]struct{}),
		sourceNotifyMIDs:   make(map[string]map[int64]struct{}, len(sources)),
		sourceErrors:       make(map[string]string),
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

// AddNotifyMIDs 实现 fetcher.Batch：记录本批次新增或播放线路实质变更的 mid（含分源去重统计）。
func (b *collectBatchContext) AddNotifyMIDs(sourceID string, mids []int64) {
	if b == nil || len(mids) == 0 {
		return
	}
	sourceID = strings.TrimSpace(sourceID)
	b.mu.Lock()
	var srcSet map[int64]struct{}
	if sourceID != "" {
		srcSet = b.sourceNotifyMIDs[sourceID]
		if srcSet == nil {
			srcSet = make(map[int64]struct{}, len(mids))
			b.sourceNotifyMIDs[sourceID] = srcSet
		}
	}
	for _, mid := range mids {
		if mid > 0 {
			b.notifyMIDs[mid] = struct{}{}
			if srcSet != nil {
				srcSet[mid] = struct{}{}
			}
		}
	}
	b.mu.Unlock()
}

// RecordSourceError 实现 fetcher.Batch：记录单站采集错误信息。
func (b *collectBatchContext) RecordSourceError(sourceID, reason string) {
	if b == nil {
		return
	}
	sourceID = strings.TrimSpace(sourceID)
	reason = strings.TrimSpace(reason)
	if sourceID == "" || reason == "" {
		return
	}
	b.mu.Lock()
	b.sourceErrors[sourceID] = reason
	b.mu.Unlock()
}

// emitSummary 构建并异步投递本次采集批次汇总通知。
func (b *collectBatchContext) emitSummary() {
	emitBatchNotify(b)
}

func (b *collectBatchContext) releaseSourceOccupy(id string) {
	id = collectSourceID(id)
	if id == "" {
		return
	}
	if b == nil {
		releaseCollectSourceIDs(id)
		return
	}
	b.mu.Lock()
	tok, hasTok := b.occupyTokens[id]
	delete(b.occupyTokens, id)
	b.mu.Unlock()
	if !hasTok {
		return
	}
	releaseCollectTokens(map[string]uint64{id: tok})
}

// ownsOccupy 当前占用令牌仍是本批次抢到的那一个。停止后被新采集重新占用时为 false。
func (b *collectBatchContext) ownsOccupy(id string) bool {
	if b == nil {
		return false
	}
	id = collectSourceID(id)
	b.mu.Lock()
	tok, ok := b.occupyTokens[id]
	b.mu.Unlock()
	if !ok {
		return false
	}
	collectingSourcesMu.Lock()
	cur, exists := collectingSources[id]
	collectingSourcesMu.Unlock()
	return exists && cur == tok
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

// finalizePrimarySource 首选采集站完成时立即刷窗并触发首页缓存预热，无需等待同批其余慢速非首选站。
func (b *collectBatchContext) finalizePrimarySource(sourceID string) error {
	if b == nil || !isPreferredCollectSource(sourceID) {
		return nil
	}
	b.mu.Lock()
	if b.primaryWarmedUp {
		b.mu.Unlock()
		return nil
	}
	b.primaryWarmedUp = true
	b.mu.Unlock()

	collectLifecycle.beginPublish()
	defer collectLifecycle.endPublish()
	publishMu.Lock()
	defer publishMu.Unlock()

	return finalizeStreamPublish(true)
}

func (b *collectBatchContext) flushAndFinalize() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	hasWork := len(b.finishedSources) > 0 || len(b.masterAffectedMIDs) > 0 || streamBuf.pendingCount() > 0
	warmupHome := false
	if !b.primaryWarmedUp {
		for id := range b.finishedSources {
			if isPreferredCollectSource(id) {
				warmupHome = true
				b.primaryWarmedUp = true
				break
			}
		}
	}
	b.finishedSources = make(map[string]model.FilmSource)
	b.masterAffectedMIDs = make(map[int64]struct{})
	b.mu.Unlock()
	if !hasWork {
		return nil
	}

	collectLifecycle.beginPublish()
	defer collectLifecycle.endPublish()
	publishMu.Lock()
	defer publishMu.Unlock()

	return finalizeStreamPublish(warmupHome)
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
