package collect

import (
	"context"
	"log"
	"server/internal/collect/engine"
	"server/internal/film/writer"
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
// 锁顺序约定：始终先 recordMu 再 mu，避免结算与删站摘除交叉死锁。
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
	page1Seeds        map[string]*engine.Page1Result
	sourceFilmCounts  map[string]int
	sourceFilmSets    map[string]map[int64]struct{}
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
		seq:              batchSeq.Add(1),
		batchID:          generateBatchID(startedAt),
		trigger:          trigger,
		tag:              tag,
		sources:          sources,
		startedAt:        startedAt,
		isStandalone:     standalone,
		pendingSealMIDs:  make(map[string][]int64, len(sources)),
		page1Seeds:       make(map[string]*engine.Page1Result, len(sources)),
		sourceFilmCounts: make(map[string]int, len(sources)),
		sourceFilmSets:   make(map[string]map[int64]struct{}, len(sources)),
		sourceHours:      make(map[string]int),
		sourceErrors:     make(map[string]string),
		sourceSkips:      make(map[string]categorySkipCount),
		finishedSources:  make(map[string]model.FilmSource),
		sourceSnapshots:  make(map[string]model.CollectRecordSourceDetail),
		occupyTokens:     snapshotOccupyTokens(sources),
		progressEpochs:   snapshotProgressEpochs(sources),
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

func (b *collectBatchContext) setPage1Seed(sourceID string, seed *engine.Page1Result) {
	if b == nil || sourceID == "" || seed == nil {
		return
	}
	b.mu.Lock()
	if b.page1Seeds == nil {
		b.page1Seeds = make(map[string]*engine.Page1Result)
	}
	b.page1Seeds[sourceID] = seed
	b.mu.Unlock()
}

func (b *collectBatchContext) takePage1Seed(sourceID string) *engine.Page1Result {
	if b == nil || sourceID == "" {
		return nil
	}
	b.mu.Lock()
	seed := b.page1Seeds[sourceID]
	delete(b.page1Seeds, sourceID)
	b.mu.Unlock()
	return seed
}

func (b *collectBatchContext) addAffectedMIDs(s *model.FilmSource, _ int, mids []int64) {
	if b == nil || s == nil || len(mids) == 0 {
		return
	}
	seen := make(map[int64]struct{}, len(mids))
	kept := make([]int64, 0, len(mids))
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if _, dup := seen[mid]; dup {
			continue
		}
		seen[mid] = struct{}{}
		kept = append(kept, mid)
	}
	if len(kept) == 0 {
		return
	}
	pref := currentPreferredSourceID()
	b.mu.Lock()
	if b.sourceDroppedLocked(s.Id) {
		b.mu.Unlock()
		return
	}
	if b.sourceFilmCounts == nil {
		b.sourceFilmCounts = make(map[string]int)
	}
	if b.sourceFilmSets == nil {
		b.sourceFilmSets = make(map[string]map[int64]struct{})
	}
	filmSet := b.sourceFilmSets[s.Id]
	if filmSet == nil {
		filmSet = make(map[int64]struct{}, len(kept))
		b.sourceFilmSets[s.Id] = filmSet
	}
	added := 0
	for _, mid := range kept {
		if _, exists := filmSet[mid]; !exists {
			filmSet[mid] = struct{}{}
			added++
		}
	}
	b.sourceFilmCounts[s.Id] += added
	// 非主站不进检索索引封口，跳过切片暂存以减少并发全量采集内存占用。
	if pref == "" || pref == s.Id {
		if b.pendingSealMIDs == nil {
			b.pendingSealMIDs = make(map[string][]int64)
		}
		b.pendingSealMIDs[s.Id] = append(b.pendingSealMIDs[s.Id], kept...)
	}
	b.mu.Unlock()

	if err := enqueueStreamPublish(s.Id, kept); err != nil {
		log.Printf("[Spider][StreamPublish] 流式刷窗失败 source=%s err=%v", s.Id, err)
	}
}

// releaseSourceFilmSet 在单站采集写库结束后立即释放该站去重集合，保留 sourceFilmCounts 供批次结算使用，避免多站全量采集时堆内存持续累积。
func (b *collectBatchContext) releaseSourceFilmSet(sourceID string) {
	if b == nil {
		return
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	b.mu.Lock()
	delete(b.sourceFilmSets, sourceID)
	b.mu.Unlock()
}

// WriteContext 为主站写库上下文注入当前批次记录 ID，非主站或无有效记录 ID 时透传原上下文。
func (b *collectBatchContext) WriteContext(ctx context.Context, s *model.FilmSource) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if b == nil || s == nil {
		return ctx
	}
	pref := currentPreferredSourceID()
	if pref != "" && pref != strings.TrimSpace(s.Id) {
		return ctx
	}
	b.mu.Lock()
	dropped := b.sourceDroppedLocked(s.Id)
	recordID := b.recordID
	b.mu.Unlock()
	if dropped || recordID == 0 {
		return ctx
	}
	return writer.WithCollectRecordID(ctx, recordID)
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
