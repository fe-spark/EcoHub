package collect

import (
	"log"
	"server/internal/model"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// publishMu 全局快照发布互斥锁，确保向 MySQL 发布快照时单次只有一个线程在执行
var publishMu sync.Mutex

var (
	activeBatchesMu sync.Mutex
	activeBatches   = make(map[*collectBatchContext]struct{})
	batchSeq        atomic.Uint64
)

type pendingLedgerFilm struct {
	sourceID string
	mid      int64
}

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

// collectBatchContext 批次上下文：封装单次采集运行的全部生命周期与状态（完全自闭环，跨批次零耦合）。
// 锁顺序约定：始终先 recordMu 再 mu，避免流式刷账与删站摘除交叉死锁。
type collectBatchContext struct {
	mu                sync.Mutex
	recordMu          sync.Mutex
	seq               uint64
	droppedSources    map[string]struct{}
	recordID          uint
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
	pendingSealMIDs   map[string][]int64
	ledgerPending     []pendingLedgerFilm
	ledgerFlushAt     time.Time
	ledgerWritten     bool
	ledgerFlushErr    error
	sourceHours       map[string]int
	sourceErrors      map[string]string
	sourceSkips       map[string]categorySkipCount
	sourceTargetPages map[string][]int
	finishedSources   map[string]model.FilmSource
	sourceSnapshots   map[string]model.CollectRecordSourceDetail
	occupyTokens      map[string]uint64
	// progressEpochs 建批次时各站的进度代次。停止时只改本代的进度，避免把同站新开的重试标成已停止。
	progressEpochs map[string]uint64
	cronTaskID     string
	runNoted       atomic.Bool
	recordStarted  bool
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
		seq:             batchSeq.Add(1),
		batchID:         generateBatchID(startedAt),
		trigger:         trigger,
		tag:             tag,
		sources:         sources,
		startedAt:       startedAt,
		isStandalone:    standalone,
		pendingSealMIDs: make(map[string][]int64, len(sources)),
		ledgerFlushAt:   time.Now(),
		sourceHours:     make(map[string]int),
		sourceErrors:    make(map[string]string),
		sourceSkips:     make(map[string]categorySkipCount),
		finishedSources: make(map[string]model.FilmSource),
		sourceSnapshots: make(map[string]model.CollectRecordSourceDetail),
		occupyTokens:    snapshotOccupyTokens(sources),
		progressEpochs:  snapshotProgressEpochs(sources),
	}

	registerActiveBatch(b)
	return b
}

func (b *collectBatchContext) noteCronCollectStarted() {
	if b == nil || b.trigger != model.CollectTriggerCron || b.runNoted.Swap(true) {
		return
	}
	taskID := strings.TrimSpace(b.cronTaskID)
	if taskID == "" {
		taskID = "sys_cron_auto_collect"
	}
	noteTaskRan(taskID, time.Now())
}

func (b *collectBatchContext) takeAffectedMIDs() []int64 {
	if b == nil {
		return nil
	}
	pref := currentPreferredSourceID()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.pendingSealMIDs) == 0 {
		return nil
	}
	var out []int64
	for sid, mids := range b.pendingSealMIDs {
		if b.sourceDroppedLocked(sid) {
			continue
		}
		if pref != "" && sid != pref {
			continue
		}
		out = append(out, mids...)
	}
	b.pendingSealMIDs = make(map[string][]int64)
	return out
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
	pref := currentPreferredSourceID()
	shouldFlush := false
	b.mu.Lock()
	if b.sourceDroppedLocked(s.Id) {
		b.mu.Unlock()
		return
	}
	// 非主站不进检索索引封口，跳过切片暂存以减少并发全量采集内存占用。
	if pref == "" || pref == s.Id {
		if b.pendingSealMIDs == nil {
			b.pendingSealMIDs = make(map[string][]int64)
		}
		b.pendingSealMIDs[s.Id] = append(b.pendingSealMIDs[s.Id], kept...)
	}
	shouldFlush = b.enqueueLedgerLocked(s.Id, kept)
	b.mu.Unlock()

	if shouldFlush {
		b.flushLedger()
	}
	if err := enqueueStreamPublish(s.Id, kept); err != nil {
		log.Printf("[Spider][StreamPublish] 流式刷窗失败 source=%s err=%v", s.Id, err)
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

type categorySkipCount struct {
	unmapped int
}

// AddCategorySkip 实现 fetcher.Batch：累计分类未映射而没有入库的部数。
func (b *collectBatchContext) AddCategorySkip(sourceID string, unmapped int) {
	if b == nil || unmapped <= 0 {
		return
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	b.mu.Lock()
	got := b.sourceSkips[sourceID]
	got.unmapped += unmapped
	b.sourceSkips[sourceID] = got
	b.mu.Unlock()
}

func (b *collectBatchContext) CategorySkip(sourceID string) int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	got := b.sourceSkips[sourceID]
	b.mu.Unlock()
	return got.unmapped
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
