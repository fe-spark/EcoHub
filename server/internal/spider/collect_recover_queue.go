package spider

import (
	"log"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/syslog"
	"server/internal/model"
)

// failureRecoverLane 合并同一采集站上的手动重试。
// 每条记录单独占站时，多选重试只有第一条能跑，其余会因为站点已被占用而被放弃。
type failureRecoverLane struct {
	mu      sync.Mutex
	pending []model.FailureRecord
	running bool
}

var failureRecoverLanes sync.Map // sourceID -> *failureRecoverLane

type failureRecoverRun func(source model.FilmSource, records []model.FailureRecord, batchCtx *collectBatchContext)

func failureRecoverLaneFor(sourceID string) *failureRecoverLane {
	lane, _ := failureRecoverLanes.LoadOrStore(sourceID, &failureRecoverLane{})
	return lane.(*failureRecoverLane)
}

// pushFailureRecover 把记录放进该站的重试队列。返回 true 时调用方要启动 worker。
func pushFailureRecover(sourceID string, fr model.FailureRecord) bool {
	lane := failureRecoverLaneFor(sourceID)
	lane.mu.Lock()
	defer lane.mu.Unlock()
	lane.pending = append(lane.pending, fr)
	if lane.running {
		return false
	}
	lane.running = true
	return true
}

func drainFailureRecover(sourceID string) []model.FailureRecord {
	value, ok := failureRecoverLanes.Load(sourceID)
	if !ok {
		return nil
	}
	lane := value.(*failureRecoverLane)
	lane.mu.Lock()
	defer lane.mu.Unlock()
	batch := lane.pending
	lane.pending = nil
	return batch
}

// finishFailureRecoverLane 在 worker 退出前调用。队列里还有记录时返回 true，调用方要再跑一轮。
func finishFailureRecoverLane(sourceID string) bool {
	value, ok := failureRecoverLanes.Load(sourceID)
	if !ok {
		return false
	}
	lane := value.(*failureRecoverLane)
	lane.mu.Lock()
	defer lane.mu.Unlock()
	if len(lane.pending) > 0 {
		return true
	}
	lane.running = false
	return false
}

func failureRecoverLaneBusy(sourceID string) bool {
	value, ok := failureRecoverLanes.Load(sourceID)
	if !ok {
		return false
	}
	lane := value.(*failureRecoverLane)
	lane.mu.Lock()
	defer lane.mu.Unlock()
	return lane.running
}

// failureRecoverBlocked 别的采集占着站点时，新的重试不能开始。
// 本站重试队列已经在跑时返回 false，后来的记录要加入同一条队列。
func failureRecoverBlocked(sourceID string) bool {
	if failureRecoverLaneBusy(sourceID) {
		return false
	}
	return isOccupiedCollectSource(sourceID)
}

var (
	failureRecoverSlotsOnce sync.Once
	failureRecoverSlots     chan struct{}
)

func acquireFailureRecoverSlot() func() {
	limit := config.CollectSourceConcurrency
	if limit <= 0 {
		return func() {}
	}
	failureRecoverSlotsOnce.Do(func() {
		failureRecoverSlots = make(chan struct{}, limit)
	})
	failureRecoverSlots <- struct{}{}
	return func() { <-failureRecoverSlots }
}

func abandonFailureRecoverPending(sourceID string) {
	for {
		batch := drainFailureRecover(sourceID)
		if len(batch) == 0 {
			return
		}
		for i := range batch {
			abandonFailureRetry(batch[i].ID)
		}
	}
}

// serveFailureRecoverLane 一次占用站点，按入队顺序重试该站当前队列里的全部记录。
// 执行期间新进来的记录留在同一次占用里继续跑，避免下一条重试被本条占站丢掉。
func serveFailureRecoverLane(sourceID string, source model.FilmSource, run failureRecoverRun) {
	defer func() {
		if finishFailureRecoverLane(sourceID) {
			go serveFailureRecoverLane(sourceID, source, run)
		}
	}()
	releaseSlot := acquireFailureRecoverSlot()
	defer releaseSlot()

	claimed := filterCollectableSources([]model.FilmSource{source}, "失败恢复")
	if len(claimed) == 0 {
		log.Printf("[Spider] 站点 %s 已在正采集队列中，跳过失败页重试", source.Id)
		abandonFailureRecoverPending(sourceID)
		return
	}
	startedAt := time.Now()
	batchCtx := newCollectBatchContext(model.CollectTriggerRecover, "失败恢复", claimed, startedAt, true)
	if err := collectLifecycle.beginSource(source.Id); err != nil {
		syslog.Errorf("[Spider] 站点 %s 无法启动失败页重试: %v", source.Id, err)
		batchCtx.close()
		abandonFailureRecoverPending(sourceID)
		return
	}
	defer collectLifecycle.endSource(source.Id)
	defer func() {
		batchCtx.markSourceFinished(source)
		if err := batchCtx.flushAndFinalize(); err != nil {
			syslog.Errorf("[Spider] 站点 %s 失败页重试缓存刷新失败: %v", source.Id, err)
		}
		batchCtx.emitSummary()
		batchCtx.close()
	}()

	for {
		batch := drainFailureRecover(sourceID)
		if len(batch) == 0 {
			return
		}
		if run != nil {
			run(source, batch, batchCtx)
		}
	}
}
