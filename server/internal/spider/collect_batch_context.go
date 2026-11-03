package spider

import (
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/model"
)

// publishMu 全局快照发布互斥锁，确保向 MySQL 发布快照时单次只有一个线程在执行
var publishMu sync.Mutex

var (
	activeBatchesMu sync.Mutex
	activeBatches   = make(map[*collectBatchContext]struct{})
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

// collectBatchContext 批次上下文：封装单次采集运行的全部生命周期与状态（完全自闭环，跨批次零耦合）
type collectBatchContext struct {
	mu                 sync.Mutex
	batchID            string
	parentBatchID      string
	hour               int
	replayFilmIDs      []int64
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
	sourceSnapshots    map[string]model.CollectRecordSourceDetail
	occupyTokens       map[string]uint64
	runNoted           atomic.Bool
	recordStarted      bool
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
		batchID:            generateBatchID(startedAt),
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
		sourceSnapshots:    make(map[string]model.CollectRecordSourceDetail),
		occupyTokens:       snapshotOccupyTokens(sources),
	}

	registerActiveBatch(b)
	return b
}

func (b *collectBatchContext) noteCronCollectStarted() {
	if b == nil || b.trigger != model.CollectTriggerCron || b.runNoted.Swap(true) {
		return
	}
	noteTaskRan("sys_cron_auto_collect", time.Now())
}

func (b *collectBatchContext) takeAffectedMIDs() []int64 {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.masterAffectedMIDs) == 0 {
		return nil
	}
	mids := make([]int64, 0, len(b.masterAffectedMIDs))
	for mid := range b.masterAffectedMIDs {
		mids = append(mids, mid)
	}
	b.masterAffectedMIDs = make(map[int64]struct{})
	return mids
}

func (b *collectBatchContext) addAffectedMIDs(s *model.FilmSource, h int, mids []int64) {
	if b == nil || len(mids) == 0 || s == nil {
		return
	}
	b.mu.Lock()
	const maxTrackedMIDs = 1500
	for _, mid := range mids {
		if mid > 0 {
			if len(b.masterAffectedMIDs) < maxTrackedMIDs {
				b.masterAffectedMIDs[mid] = struct{}{}
			} else {
				b.masterAffectedMIDs[int64(maxTrackedMIDs+1)] = struct{}{}
				break
			}
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
	if hasTok && tok > 0 {
		releaseCollectTokens(map[string]uint64{id: tok})
	} else {
		releaseCollectSourceIDs(id)
	}
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

func (b *collectBatchContext) recordSourceSnapshot(detail model.CollectRecordSourceDetail) {
	if b == nil || strings.TrimSpace(detail.SourceID) == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sourceSnapshots[detail.SourceID] = detail
}

func (b *collectBatchContext) getSourceSnapshot(sourceID string) (model.CollectRecordSourceDetail, bool) {
	if b == nil {
		return model.CollectRecordSourceDetail{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.sourceSnapshots[sourceID]
	return d, ok
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

	return finalizeStreamPublish(b.takeAffectedMIDs, true)
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
	b.mu.Unlock()
	if !hasWork {
		return nil
	}

	collectLifecycle.beginPublish()
	defer collectLifecycle.endPublish()
	publishMu.Lock()
	defer publishMu.Unlock()

	return finalizeStreamPublish(b.takeAffectedMIDs, warmupHome)
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
