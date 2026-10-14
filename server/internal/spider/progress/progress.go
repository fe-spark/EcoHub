package progress

import (
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
	if IsTerminalStatus(progress.Status) {
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
		return isActiveStatus(progress.Status)
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
		active := isActiveStatus(state.data.Status)
		state.mu.RUnlock()
		if active {
			hasActive = true
			return false
		}
		return true
	})
	return hasActive
}
