package spider

import (
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/model"
	filmcache "server/internal/repository/film/cache"
	filmsnapshot "server/internal/repository/film/snapshot"
)

// publishMu 全局快照发布互斥锁，确保向 MySQL 发布快照时单次只有一个线程在执行
var publishMu sync.Mutex

var (
	activeBatchesMu sync.Mutex
	activeBatches   = make(map[*collectBatchContext]struct{})
	batchSeq        atomic.Uint64
)

func registerActiveBatch(b *collectBatchContext) {
	if b == nil {
		return
	}
	activeBatchesMu.Lock()
	defer activeBatchesMu.Unlock()
	activeBatches[b] = struct{}{}
}

func activeBatchCount() int {
	activeBatchesMu.Lock()
	defer activeBatchesMu.Unlock()
	return len(activeBatches)
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
	mu                sync.Mutex
	recordMu          sync.Mutex
	seq               uint64
	droppedSources    map[string]struct{}
	batchID           string
	parentBatchID     string
	hour              int
	replayFilmIDs     []int64
	trigger           string
	tag               string
	sources           []model.FilmSource
	startedAt         time.Time
	isStandalone      bool
	primaryWarmedUp   bool
	affectedMIDs      []int64
	notifyMIDs        map[int64]struct{}
	sourceNotifyMIDs  map[string]map[int64]struct{}
	filmMIDs          map[int64]struct{}
	sourceFilmMIDs    map[string]map[int64]struct{}
	pendingFilms      map[string][]int64
	sourceHours       map[string]int
	sourceErrors      map[string]string
	sourceSkips       map[string]categorySkipCount
	sourceTargetPages map[string][]int
	progressDirty     bool
	flushing          bool
	lastFlush         time.Time
	flushWG           sync.WaitGroup
	finishedSources   map[string]model.FilmSource
	sourceSnapshots   map[string]model.CollectRecordSourceDetail
	occupyTokens      map[string]uint64
	runNoted          atomic.Bool
	recordStarted     bool
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
		seq:              batchSeq.Add(1),
		batchID:          generateBatchID(startedAt),
		trigger:          trigger,
		tag:              tag,
		sources:          sources,
		startedAt:        startedAt,
		isStandalone:     standalone,
		notifyMIDs:       make(map[int64]struct{}),
		sourceNotifyMIDs: make(map[string]map[int64]struct{}, len(sources)),
		filmMIDs:         make(map[int64]struct{}),
		sourceFilmMIDs:   make(map[string]map[int64]struct{}, len(sources)),
		pendingFilms:     make(map[string][]int64),
		sourceHours:      make(map[string]int),
		sourceErrors:     make(map[string]string),
		sourceSkips:      make(map[string]categorySkipCount),
		finishedSources:  make(map[string]model.FilmSource),
		sourceSnapshots:  make(map[string]model.CollectRecordSourceDetail),
		occupyTokens:     snapshotOccupyTokens(sources),
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
	mids := b.affectedMIDs
	b.affectedMIDs = nil
	if len(b.droppedSources) == 0 {
		return mids
	}
	kept := mids[:0]
	for _, mid := range mids {
		if b.midListedLocked(mid) {
			kept = append(kept, mid)
		}
	}
	return kept
}

func (b *collectBatchContext) addAffectedMIDs(s *model.FilmSource, _ int, mids []int64) {
	if b == nil || s == nil || len(mids) == 0 {
		return
	}
	kept := make([]int64, 0, len(mids))
	for _, mid := range mids {
		if mid > 0 {
			kept = append(kept, mid)
		}
	}
	if len(kept) == 0 {
		return
	}
	b.mu.Lock()
	if b.sourceDroppedLocked(s.Id) {
		b.mu.Unlock()
		return
	}
	b.affectedMIDs = append(b.affectedMIDs, kept...)
	b.trackFilmsLocked(s.Id, kept)
	b.mu.Unlock()
	if err := enqueueStreamPublish(s.Id, kept); err != nil {
		log.Printf("[Spider][StreamPublish] 流式刷窗失败 source=%s err=%v", s.Id, err)
	}
	b.maybeFlushRecord()
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
	if b.sourceDroppedLocked(sourceID) {
		b.mu.Unlock()
		return
	}
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

type categorySkipCount struct {
	unmapped int
	disabled int
}

// AddCategorySkip 实现 fetcher.Batch：累计分类未入库的部数。
func (b *collectBatchContext) AddCategorySkip(sourceID string, unmapped, disabled int) {
	if b == nil || (unmapped <= 0 && disabled <= 0) {
		return
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	b.mu.Lock()
	got := b.sourceSkips[sourceID]
	got.unmapped += unmapped
	got.disabled += disabled
	b.sourceSkips[sourceID] = got
	b.mu.Unlock()
}

func (b *collectBatchContext) CategorySkip(sourceID string) (unmapped, disabled int) {
	if b == nil {
		return 0, 0
	}
	b.mu.Lock()
	got := b.sourceSkips[sourceID]
	b.mu.Unlock()
	return got.unmapped, got.disabled
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
	b.sourceSnapshots[detail.SourceID] = detail
	b.mu.Unlock()
	// 站点结束后立刻落库，批次还在跑时重启也能看见这一站已经写入的影片。
	b.flushRecordSync()
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

// takeSourceMIDs 取出这一站尚未封口的影片编号，其它站的编号留在批次里。
func (b *collectBatchContext) takeSourceMIDs(sourceID string) []int64 {
	if b == nil {
		return nil
	}
	sourceID = strings.TrimSpace(sourceID)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sourceDroppedLocked(sourceID) {
		return nil
	}
	set := b.sourceFilmMIDs[sourceID]
	if len(set) == 0 || len(b.affectedMIDs) == 0 {
		return nil
	}
	taken := make([]int64, 0, len(set))
	seen := make(map[int64]struct{}, len(set))
	kept := make([]int64, 0, len(b.affectedMIDs))
	for _, mid := range b.affectedMIDs {
		if _, ok := set[mid]; !ok {
			kept = append(kept, mid)
			continue
		}
		if _, dup := seen[mid]; dup {
			continue
		}
		seen[mid] = struct{}{}
		taken = append(taken, mid)
	}
	b.affectedMIDs = kept
	return taken
}

// sealFinishedSource 刷窗后把这一站送进全局封口队列。当时它是主站才预热首页。
func (b *collectBatchContext) sealFinishedSource(source model.FilmSource) error {
	if b == nil {
		return nil
	}
	source.Id = strings.TrimSpace(source.Id)
	if source.Id == "" {
		return nil
	}
	collectLifecycle.beginPublish()
	defer collectLifecycle.endPublish()
	publishMu.Lock()
	defer publishMu.Unlock()
	if _, err := drainStreamPublishLocked(true, false); err != nil {
		return err
	}
	mids := b.takeSourceMIDs(source.Id)
	scheduleSearchReseal(mids)
	filmcache.ClearTVBoxConfigCache()
	b.warmPrimaryHome(source.Id)
	return nil
}

func (b *collectBatchContext) warmPrimaryHome(sourceID string) {
	b.mu.Lock()
	if !isPreferredCollectSource(sourceID) || b.primaryWarmedUp {
		b.mu.Unlock()
		return
	}
	b.primaryWarmedUp = true
	b.mu.Unlock()
	filmsnapshot.FlushSnapshotCacheInvalidation()
	if version := filmsnapshot.GetActiveReadModelVersion(); version != "" {
		filmsnapshot.NotifySnapshotPublished(version)
	}
}

func (b *collectBatchContext) flushAndFinalize() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	hasWork := len(b.finishedSources) > 0 || len(b.affectedMIDs) > 0 || streamBuf.pendingCount() > 0
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
	b.flushWG.Wait()
	unregisterActiveBatch(b)
	b.mu.Lock()
	tokens := b.occupyTokens
	b.occupyTokens = nil
	b.mu.Unlock()
	releaseCollectTokens(tokens)
}

func (b *collectBatchContext) getTargetPages(sourceID string) []int {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sourceTargetPages == nil {
		return nil
	}
	return b.sourceTargetPages[sourceID]
}

func (b *collectBatchContext) withTargetPages(targetPages map[string][]int) *collectBatchContext {
	if b == nil || len(targetPages) == 0 {
		return b
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sourceTargetPages = make(map[string][]int, len(targetPages))
	for k, v := range targetPages {
		b.sourceTargetPages[k] = append([]int(nil), v...)
	}
	return b
}
