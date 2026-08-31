package spider

import (
	"sort"
	"strings"
	"sync"
	"time"

	"server/internal/model"
	"server/internal/notify"
)

// publishMu 全局快照发布互斥锁，确保向 MySQL 发布快照时单次只有一个线程在执行
var publishMu sync.Mutex

// collectBatchContext 批次上下文：封装单次采集运行的全部生命周期与状态（完全自闭环，跨批次零耦合）
type collectBatchContext struct {
	mu                 sync.Mutex
	trigger            string
	tag                string
	sources            []model.FilmSource
	batch              *notify.ChangeBatch
	startedAt          time.Time
	isStandalone       bool
	affectedMIDs       map[int64]struct{}
	masterAffectedMIDs map[int64]struct{}
	pendingMasterMIDs  map[string]map[int64]struct{}
	finishedSources    map[string]model.FilmSource
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
	return &collectBatchContext{
		trigger:            trigger,
		tag:                tag,
		sources:            sources,
		batch:              batch,
		startedAt:          startedAt,
		isStandalone:       standalone,
		affectedMIDs:       make(map[int64]struct{}),
		masterAffectedMIDs: make(map[int64]struct{}),
		pendingMasterMIDs:  make(map[string]map[int64]struct{}),
		finishedSources:    make(map[string]model.FilmSource),
	}
}

func (b *collectBatchContext) beginMasterRebuild(sourceID string) {
	if b == nil {
		return
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pendingMasterMIDs[sourceID] = make(map[int64]struct{})
}

func (b *collectBatchContext) publishPendingMasterMIDs(sourceID string) {
	if b == nil {
		return
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	pending := b.pendingMasterMIDs[sourceID]
	delete(b.pendingMasterMIDs, sourceID)
	for mid := range pending {
		if mid > 0 {
			b.affectedMIDs[mid] = struct{}{}
			b.masterAffectedMIDs[mid] = struct{}{}
		}
	}
}

func (b *collectBatchContext) discardPendingMasterMIDs(sourceID string) {
	if b == nil {
		return
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pendingMasterMIDs, sourceID)
}

func (b *collectBatchContext) addAffectedMIDs(s *model.FilmSource, h int, mids []int64) {
	if b == nil || len(mids) == 0 || s == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if s.Grade == model.MasterCollect && h < 0 {
		pending := b.pendingMasterMIDs[s.Id]
		if pending == nil {
			pending = make(map[int64]struct{})
			b.pendingMasterMIDs[s.Id] = pending
		}
		for _, mid := range mids {
			if mid > 0 {
				pending[mid] = struct{}{}
			}
		}
		return
	}

	for _, mid := range mids {
		if mid > 0 {
			b.affectedMIDs[mid] = struct{}{}
			if s.Grade == model.MasterCollect {
				b.masterAffectedMIDs[mid] = struct{}{}
			}
		}
	}
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
	if len(b.finishedSources) == 0 && len(b.affectedMIDs) == 0 {
		b.mu.Unlock()
		return nil
	}

	sources := make([]model.FilmSource, 0, len(b.finishedSources))
	for _, s := range b.finishedSources {
		sources = append(sources, s)
	}
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].Grade == sources[j].Grade {
			return sources[i].Id < sources[j].Id
		}
		return sources[i].Grade == model.MasterCollect
	})

	affectedMIDs := make([]int64, 0, len(b.affectedMIDs))
	for mid := range b.affectedMIDs {
		if mid > 0 {
			affectedMIDs = append(affectedMIDs, mid)
		}
	}
	sort.Slice(affectedMIDs, func(i, j int) bool { return affectedMIDs[i] < affectedMIDs[j] })

	masterMIDs := make([]int64, 0, len(b.masterAffectedMIDs))
	for mid := range b.masterAffectedMIDs {
		if mid > 0 {
			masterMIDs = append(masterMIDs, mid)
		}
	}
	sort.Slice(masterMIDs, func(i, j int) bool { return masterMIDs[i] < masterMIDs[j] })
	finishedMap := b.finishedSources
	b.finishedSources = make(map[string]model.FilmSource)
	b.affectedMIDs = make(map[int64]struct{})
	b.masterAffectedMIDs = make(map[int64]struct{})
	b.mu.Unlock()

	markSourcesFinalizing(finishedMap)

	publishMu.Lock()
	defer publishMu.Unlock()

	_, _, err := finalizeCollectRun(sources, affectedMIDs, masterMIDs)
	if err != nil {
		markSourcesFinalizeFailed(finishedMap)
		return err
	}
	markSourcesPublished(finishedMap)
	return nil
}

func (b *collectBatchContext) emitSummary(finalizeErr error) {
	if b == nil {
		return
	}
	emitBatchSummaryForSources(b.batch, b.trigger, b.sources, b.startedAt, finalizeErr)
}
