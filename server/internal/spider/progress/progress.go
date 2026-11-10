package progress

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"server/internal/model"
	"server/internal/repository"
)

// occupyChecker 由编排层注入：排队占用中的 starting 不是僵尸。
var occupyChecker func(string) bool

// SetOccupyChecker 注册占用查询。传 nil 清除（测试用）。
func SetOccupyChecker(fn func(string) bool) {
	occupyChecker = fn
}

func sourceOccupied(id string) bool {
	if occupyChecker == nil {
		return false
	}
	return occupyChecker(id)
}

var store sync.Map

type progressState struct {
	mu      sync.RWMutex
	data    model.CollectProgress
	updated time.Time
	// probed 总页已写入。新开一轮采集时清掉。
	probed bool
	// epoch 每次新开采集加一。旧运行的进度写入对不上就丢弃。
	epoch uint64
	// sealOutcome 封口周期结束后回落的状态：done 或 stopped。
	sealOutcome string
}

// Ensure 初始化站点进度条目：已存在则刷新名称与时间，不存在则按 starting 创建。
func Ensure(sourceID string, name string) {
	ensure(sourceID, name)
}

// ensure 内部实现，返回状态句柄供同包状态机复用。
func ensure(sourceID string, name string) *progressState {
	if val, ok := store.Load(sourceID); ok {
		state := val.(*progressState)
		state.mu.Lock()
		state.data.Id = sourceID
		if name != "" {
			state.data.Name = name
		}
		state.updated = time.Now()
		state.mu.Unlock()
		return state
	}
	state := &progressState{data: model.CollectProgress{Id: sourceID, Name: name, Status: StatusStarting}, updated: time.Now()}
	actual, _ := store.LoadOrStore(sourceID, state)
	return actual.(*progressState)
}

// SetPageTotal 写入探测结果。只接受仍在排队的站，并在同一次锁里标成已探测。0 页直接标完成。
func SetPageTotal(sourceID string, pageCount int) {
	setPageTotal(sourceID, pageCount, nil)
}

// SetPageTotalIfEpoch 同 SetPageTotal，但只写 epoch 这一代的进度，返回是否属于这一代。
// 旧批次探页返回较晚时，同站可能已被新的采集接手，不能把新一轮的总页改掉。
func SetPageTotalIfEpoch(sourceID string, epoch uint64, pageCount int) bool {
	return setPageTotal(sourceID, pageCount, &epoch)
}

func setPageTotal(sourceID string, pageCount int, epoch *uint64) bool {
	val, ok := store.Load(sourceID)
	if !ok {
		return true
	}
	state := val.(*progressState)
	state.mu.Lock()
	if epoch != nil && state.epoch != *epoch {
		state.mu.Unlock()
		return false
	}
	accepted := state.data.Status == StatusStarting
	if accepted {
		state.data.Total = pageCount
		state.data.Current = 0
		state.data.Success = 0
		state.data.Failed = 0
		state.data.FailedPages = nil
		if pageCount <= 0 {
			state.data.Status = StatusDone
		}
		state.probed = true
		state.updated = time.Now()
	}
	state.mu.Unlock()
	if accepted {
		NotifyChange()
	}
	return true
}

// PageTotal 返回已探到的总页。尚未探测返回 -1。
func PageTotal(sourceID string) int {
	val, ok := store.Load(sourceID)
	if !ok {
		return -1
	}
	state := val.(*progressState)
	state.mu.RLock()
	defer state.mu.RUnlock()
	if !state.probed {
		return -1
	}
	return state.data.Total
}

// PageSuccess 返回指定站点的成功页数。没有进度返回 0。
func PageSuccess(sourceID string) int {
	val, ok := store.Load(sourceID)
	if !ok {
		return 0
	}
	state := val.(*progressState)
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.data.Success
}

func Update(sourceID string, update func(*model.CollectProgress)) {
	if val, ok := store.Load(sourceID); ok {
		state := val.(*progressState)
		state.mu.Lock()
		update(&state.data)
		state.updated = time.Now()
		state.mu.Unlock()
		NotifyChange()
	}
}

type runEpochKey struct{}

// WithRunEpoch 把本次运行的代次放进采集上下文。
func WithRunEpoch(ctx context.Context, epoch uint64) context.Context {
	return context.WithValue(ctx, runEpochKey{}, epoch)
}

// Epoch 当前进度代次。尚未建进度时为 0。
func Epoch(sourceID string) uint64 {
	val, ok := store.Load(sourceID)
	if !ok {
		return 0
	}
	state := val.(*progressState)
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.epoch
}

// UpdateRun 只改本代、且仍在采集中的进度。停止中和终态保持原样，避免收尾把界面改回去。
func UpdateRun(ctx context.Context, sourceID string, update func(*model.CollectProgress)) bool {
	epoch, ok := ctx.Value(runEpochKey{}).(uint64)
	if !ok {
		Update(sourceID, update)
		return true
	}
	val, exists := store.Load(sourceID)
	if !exists {
		return false
	}
	state := val.(*progressState)
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.epoch != epoch || state.data.Status == StatusStopping || IsTerminalStatus(state.data.Status) {
		return false
	}
	update(&state.data)
	state.updated = time.Now()
	NotifyChange()
	return true
}

// FinishRun 本代运行真正退出。停止中改为已停止；出错且尚未终态则改为失败。代次不符说明已有新采集。
func FinishRun(ctx context.Context, sourceID string, retErr error) {
	epoch, ok := ctx.Value(runEpochKey{}).(uint64)
	if !ok {
		return
	}
	val, exists := store.Load(sourceID)
	if !exists {
		return
	}
	state := val.(*progressState)
	state.mu.Lock()
	changed := false
	if state.epoch == epoch {
		switch {
		case state.data.Status == StatusStopping:
			state.data.Status = StatusStopped
			changed = true
		case retErr != nil && !IsTerminalStatus(state.data.Status) && state.data.Status != StatusSealing:
			state.data.Status = StatusFailed
			changed = true
		}
	}
	if changed {
		state.updated = time.Now()
	}
	state.mu.Unlock()
	if changed {
		NotifyChange()
	}
}

func Snapshot(sourceID string) (model.CollectProgress, bool) {
	if val, ok := store.Load(sourceID); ok {
		state := val.(*progressState)
		state.mu.RLock()
		data := state.data
		state.mu.RUnlock()
		return data, true
	}
	return model.CollectProgress{}, false
}

// 采集进度状态机（SourceJob）：
//
//	starting → running → sealing → done / stopped / failed
//
// sealing 表示抓页已结束、等检索索引封口；封口周期结束才收成 done。
const (
	StatusStarting = "starting"
	StatusRunning  = "running"
	StatusStopping = "stopping"
	StatusSealing  = "sealing"
	StatusDone     = "done"
	StatusFailed   = "failed"
	StatusStopped  = "stopped"
)

func isActiveStatus(status string) bool {
	switch status {
	case StatusStarting, StatusRunning:
		return true
	default:
		return false
	}
}

// isInFlightStatus 抓页、停止收尾、检索封口都算这次采集还没结束。
func isInFlightStatus(status string) bool {
	return isActiveStatus(status) || status == StatusStopping || status == StatusSealing
}

func IsTerminalStatus(status string) bool {
	switch status {
	case StatusDone, StatusFailed, StatusStopped:
		return true
	default:
		return false
	}
}

// abandonedOccupied 没任务的排队才查占用。调用方不能持有该站点的进度锁，占用查询会再读进度。
func abandonedOccupied(id, status string, live bool) bool {
	if live || status != StatusStarting {
		return false
	}
	return sourceOccupied(id)
}

// shouldMarkAbandoned 没任务的采集中，以及没任务也没占用的排队，当场清掉。
func shouldMarkAbandoned(status string, live, occupied bool) bool {
	if live {
		return false
	}
	if status == StatusStarting {
		return !occupied
	}
	return status == StatusRunning
}

func IsStopped(sourceID string) bool {
	if progress, ok := Snapshot(sourceID); ok {
		return progress.Status == StatusStopped
	}
	return false
}

func IsStopping(sourceID string) bool {
	if progress, ok := Snapshot(sourceID); ok {
		return progress.Status == StatusStopping
	}
	return false
}

func IsStarting(sourceID string) bool {
	if progress, ok := Snapshot(sourceID); ok {
		return progress.Status == StatusStarting
	}
	return false
}

func IsAlreadyQueuedOrRunning(sourceID string) bool {
	if _, ok := tasks.Load(sourceID); ok {
		return true
	}
	// 停止中的收尾还没退出。这段时间和按钮上的 loading 对齐，退出后才能再开。
	if snap, ok := Snapshot(sourceID); ok && (snap.Status == StatusStopping || snap.Status == StatusSealing) {
		return true
	}
	if refreshAndIsBlockingProgress(sourceID) {
		return true
	}
	return false
}

func refreshAndIsBlockingProgress(sourceID string) bool {
	val, ok := store.Load(sourceID)
	if !ok {
		return false
	}
	state := val.(*progressState)
	state.mu.Lock()
	status := state.data.Status
	if !isActiveStatus(status) {
		state.mu.Unlock()
		return false
	}
	_, live := tasks.Load(sourceID)
	if live {
		state.mu.Unlock()
		return true
	}
	state.mu.Unlock()

	occupied := abandonedOccupied(sourceID, status, live)

	state.mu.Lock()
	status = state.data.Status
	if !isActiveStatus(status) {
		state.mu.Unlock()
		return false
	}
	_, live = tasks.Load(sourceID)
	if live {
		state.mu.Unlock()
		return true
	}
	if shouldMarkAbandoned(status, live, occupied) {
		state.data.Status = StatusFailed
		state.updated = time.Now()
		state.mu.Unlock()
		log.Printf("[Spider] 进度清理 source=%s status=%s live=%t occupied=%t -> failed",
			sourceID, status, live, occupied)
		return false
	}
	state.mu.Unlock()
	return true
}

// MarkSourceSealingIfRun 本代正常跑完先进入待检索，等封口周期结束再收成完成。
// 停止中留给 FinishRun 收成已停止。
func MarkSourceSealingIfRun(ctx context.Context, sourceID string) bool {
	return markSealing(ctx, sourceID, StatusDone)
}

// MarkStoppedAsSealing 用户停止后也先进待检索，封口周期结束时收成已停止。
func MarkStoppedAsSealing(ctx context.Context, sourceID string) bool {
	return markSealing(ctx, sourceID, StatusStopped)
}

func markSealing(ctx context.Context, sourceID, outcome string) bool {
	epoch, ok := ctx.Value(runEpochKey{}).(uint64)
	if !ok {
		return false
	}
	val, exists := store.Load(sourceID)
	if !exists {
		return false
	}
	state := val.(*progressState)
	state.mu.Lock()
	if state.epoch != epoch {
		state.mu.Unlock()
		return false
	}
	switch state.data.Status {
	case StatusRunning, StatusStarting, StatusStopping:
	default:
		state.mu.Unlock()
		return false
	}
	if outcome == StatusDone && state.data.Status == StatusStopping {
		state.mu.Unlock()
		return false
	}
	state.data.Status = StatusSealing
	state.sealOutcome = outcome
	state.updated = time.Now()
	state.mu.Unlock()
	NotifyChange()
	FlushHotpathSideEffects(sourceID)
	return true
}

// MarkSealed 封口周期结束，把等检索的站点收成最终状态。已经重开或失败的保持原状。
func MarkSealed(sourceIDs []string) {
	now := time.Now()
	changed := false
	for _, sourceID := range sourceIDs {
		val, ok := store.Load(sourceID)
		if !ok {
			continue
		}
		state := val.(*progressState)
		state.mu.Lock()
		if state.data.Status == StatusSealing {
			if state.sealOutcome == StatusStopped {
				state.data.Status = StatusStopped
			} else {
				state.data.Status = StatusDone
			}
			state.data.Sealed = true
			state.sealOutcome = ""
			state.updated = now
			changed = true
		}
		state.mu.Unlock()
	}
	if changed {
		NotifyChange()
	}
}

func IsSealing(sourceID string) bool {
	if progress, ok := Snapshot(sourceID); ok {
		return progress.Status == StatusSealing
	}
	return false
}

func FlushHotpathSideEffects(sourceIDs ...string) {
	if len(sourceIDs) == 0 {
		repository.FlushCollectSourceStats()
		return
	}
	repository.FlushCollectSourceStats(sourceIDs...)
}

// 返回各站标记后的进度代次，供调用方在同一时刻记下"这一轮属于我"。
func MarkSourcesCollectStarting(sources []model.FilmSource, queueID string) map[string]uint64 {
	queueID = strings.TrimSpace(queueID)
	epochs := make(map[string]uint64, len(sources))
	for _, source := range sources {
		state := ensure(source.Id, source.Name)
		state.mu.Lock()
		state.data.Total = 0
		state.data.Current = 0
		state.data.Success = 0
		state.data.Failed = 0
		state.data.FailedPages = nil
		state.data.Sealed = false
		state.sealOutcome = ""
		state.probed = false
		state.data.Status = StatusStarting
		state.data.QueueId = queueID
		state.epoch++
		epochs[source.Id] = state.epoch
		state.updated = time.Now()
		state.mu.Unlock()
	}
	NotifyChange()
	return epochs
}

// MarkStoppedIfEpoch 仅当进度仍是 epoch 这一代且在排队/运行中时标成已停止，返回是否属于这一代。
// 判断和修改在同一把锁里完成，不会和新开采集的 MarkSourcesCollectStarting 交错。
func MarkStoppedIfEpoch(sourceID string, epoch uint64) bool {
	return markActiveIfEpoch(sourceID, epoch, StatusStopped)
}

// MarkFailedIfEpoch 仅当进度仍是 epoch 这一代且在排队/运行中时标成失败，返回是否属于这一代。
// 旧批次探页失败晚于一键终止 + 重试时，不能把重试的进度改成失败。
func MarkFailedIfEpoch(sourceID string, epoch uint64) bool {
	return markActiveIfEpoch(sourceID, epoch, StatusFailed)
}

func markActiveIfEpoch(sourceID string, epoch uint64, status string) bool {
	val, ok := store.Load(sourceID)
	if !ok {
		return true
	}
	state := val.(*progressState)
	state.mu.Lock()
	if state.epoch != epoch {
		state.mu.Unlock()
		return false
	}
	changed := false
	if state.data.Status == StatusStarting || state.data.Status == StatusRunning {
		state.data.Status = status
		state.updated = time.Now()
		changed = true
	}
	state.mu.Unlock()
	if changed {
		NotifyChange()
	}
	return true
}

func MarkStopped(sourceID string) {
	Update(sourceID, func(progress *model.CollectProgress) {
		if progress.Status == StatusStarting || progress.Status == StatusRunning {
			progress.Status = StatusStopped
		}
	})
}
