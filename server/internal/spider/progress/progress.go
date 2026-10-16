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
	// epoch 每次新开采集加一。旧运行的进度写入对不上就丢弃。
	epoch uint64
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
		case retErr != nil && !IsTerminalStatus(state.data.Status):
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
//	starting → running → done / stopped / failed
const (
	StatusStarting = "starting"
	StatusRunning  = "running"
	StatusStopping = "stopping"
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

// isInFlightStatus 含停止后的收尾。收尾期间仍算这次运行未结束。删除会先等到它退出。
func isInFlightStatus(status string) bool {
	return isActiveStatus(status) || status == StatusStopping
}

func IsTerminalStatus(status string) bool {
	switch status {
	case StatusDone, StatusFailed, StatusStopped:
		return true
	default:
		return false
	}
}

func shouldMarkStale(sourceID, status string, live bool, age, staleAfter time.Duration) bool {
	if age < staleAfter {
		return false
	}
	// 占用中的 starting = 排队等并发槽；无占用且无 live 才是遗弃进度。
	if status == StatusStarting {
		return !live && !sourceOccupied(sourceID)
	}
	if live && status == StatusRunning {
		return false
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
	if snap, ok := Snapshot(sourceID); ok && snap.Status == StatusStopping {
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
	now := time.Now()
	staleAfter := staleDuration()

	state.mu.Lock()
	if !isActiveStatus(state.data.Status) {
		state.mu.Unlock()
		return false
	}
	_, live := tasks.Load(sourceID)
	age := now.Sub(state.updated)
	if live && (state.data.Status == StatusRunning || state.data.Status == StatusStarting) {
		state.mu.Unlock()
		return true
	}
	if shouldMarkStale(sourceID, state.data.Status, live, age, staleAfter) {
		old := state.data.Status
		state.data.Status = StatusFailed
		state.updated = now
		state.mu.Unlock()
		log.Printf("[Spider] 进度超时清理 source=%s status=%s age=%s -> failed",
			sourceID, old, age.Round(time.Second))
		return false
	}
	state.mu.Unlock()
	return true
}

func MarkSourceDone(sourceID string) {
	Update(sourceID, func(progress *model.CollectProgress) {
		if progress.Status == StatusRunning || progress.Status == StatusStarting {
			progress.Status = StatusDone
		}
	})
	FlushHotpathSideEffects(sourceID)
}

// MarkSourceDoneIfRun 本代正常跑完才标完成。停止中留给 FinishRun 收成已停止。
func MarkSourceDoneIfRun(ctx context.Context, sourceID string) {
	if !UpdateRun(ctx, sourceID, func(progress *model.CollectProgress) {
		if progress.Status == StatusRunning || progress.Status == StatusStarting {
			progress.Status = StatusDone
		}
	}) {
		return
	}
	FlushHotpathSideEffects(sourceID)
}

func FlushHotpathSideEffects(sourceIDs ...string) {
	if len(sourceIDs) == 0 {
		repository.FlushCollectSourceStats()
		return
	}
	repository.FlushCollectSourceStats(sourceIDs...)
}

func MarkSourcesCollectStarting(sources []model.FilmSource, queueID string) {
	queueID = strings.TrimSpace(queueID)
	for _, source := range sources {
		state := ensure(source.Id, source.Name)
		state.mu.Lock()
		state.data.Total = 0
		state.data.Current = 0
		state.data.Success = 0
		state.data.Failed = 0
		state.data.Status = StatusStarting
		state.data.QueueId = queueID
		state.epoch++
		state.updated = time.Now()
		state.mu.Unlock()
	}
	NotifyChange()
}

func MarkStopped(sourceID string) {
	Update(sourceID, func(progress *model.CollectProgress) {
		if progress.Status == StatusStarting || progress.Status == StatusRunning {
			progress.Status = StatusStopped
		}
	})
}

// StampPageRunning 记录正在抓取的页码。终态不可被 worker 写回 running。
func StampPageRunning(progress *model.CollectProgress, page int) {
	if IsTerminalStatus(progress.Status) || progress.Status == StatusStopping {
		return
	}
	if page > progress.Current {
		progress.Current = page
	}
	progress.Status = StatusRunning
	NotifyChange()
}

func GetActiveTaskProgress() []model.CollectProgress {
	StartHousekeeping()

	list := make([]model.CollectProgress, 0)
	seen := make(map[string]struct{})
	staleAfter := staleDuration()
	now := time.Now()

	tasks.Range(func(key, value any) bool {
		id := key.(string)
		seen[id] = struct{}{}
		if progress, ok := Snapshot(id); ok {
			list = append(list, progress)
			return true
		}
		list = append(list, model.CollectProgress{Id: id, Status: StatusRunning})
		return true
	})
	store.Range(func(key, value any) bool {
		id := key.(string)
		if _, ok := seen[id]; ok {
			return true
		}
		state := value.(*progressState)
		state.mu.Lock()
		progress := state.data
		age := now.Sub(state.updated)

		if progress.Status == StatusStopping {
			state.mu.Unlock()
			list = append(list, progress)
			return true
		}

		if isActiveStatus(progress.Status) {
			_, live := tasks.Load(id)
			if shouldMarkStale(id, progress.Status, live, age, staleAfter) {
				old := progress.Status
				progress.Status = StatusFailed
				state.data.Status = StatusFailed
				state.updated = now
				log.Printf("[Spider] 进度超时清理 source=%s status=%s age=%s -> failed", id, old, age.Round(time.Second))
				state.mu.Unlock()
				list = append(list, progress)
				return true
			}
			state.mu.Unlock()
			list = append(list, progress)
			return true
		}

		if IsTerminalStatus(progress.Status) {
			state.mu.Unlock()
			list = append(list, progress)
			return true
		}
		state.mu.Unlock()
		return true
	})

	maybePurgeTerminal(now)
	if n := len(list); n > 0 {
		filtered := list[:0]
		for _, p := range list {
			if IsTerminalStatus(p.Status) {
				if _, still := store.Load(p.Id); !still {
					continue
				}
			}
			filtered = append(filtered, p)
		}
		list = filtered
	}

	return list
}

func IsTaskRunning(id string) bool {
	if _, ok := tasks.Load(id); ok {
		return true
	}
	if progress, ok := Snapshot(id); ok {
		return isInFlightStatus(progress.Status)
	}
	return false
}

func IsAnyTaskRunning() bool {
	found := false
	tasks.Range(func(key, value any) bool {
		found = true
		return false
	})
	if found {
		return true
	}
	hasActive := false
	store.Range(func(key, value any) bool {
		state := value.(*progressState)
		state.mu.RLock()
		active := isInFlightStatus(state.data.Status)
		state.mu.RUnlock()
		if active {
			hasActive = true
			return false
		}
		return true
	})
	return hasActive
}
