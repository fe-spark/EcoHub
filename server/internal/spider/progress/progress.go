package progress

import (
	"log"
	"strings"
	"sync"
	"time"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/film/writer"
)

// staleNotifier 由采集编排层注入，用于在进度超时被判定失败时广播通知。
// 未注入时仅落日志，不影响状态机行为。
var staleNotifier = func(sourceID, sourceName, oldStatus string, age time.Duration) {}

// SetStaleNotifier 注册进度超时通知回调。
func SetStaleNotifier(fn func(sourceID, sourceName, oldStatus string, age time.Duration)) {
	if fn == nil {
		return
	}
	staleNotifier = fn
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
//	starting → running → page_done → waiting_publish → finalizing → done
//	                ↘ stopped / failed
const (
	StatusStarting       = "starting"
	StatusRunning        = "running"
	StatusPageDone       = "page_done"
	StatusWaitingPublish = "waiting_publish"
	StatusFinalizing     = "finalizing"
	StatusDone           = "done"
	StatusFailed         = "failed"
	StatusStopped        = "stopped"
)

func isActiveStatus(status string) bool {
	switch status {
	case StatusStarting, StatusRunning, StatusPageDone,
		StatusWaitingPublish, StatusFinalizing:
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

func isPostFetchStatus(status string) bool {
	switch status {
	case StatusPageDone, StatusWaitingPublish, StatusFinalizing:
		return true
	default:
		return false
	}
}

func shouldMarkStale(status string, live bool, age, staleAfter time.Duration) bool {
	if age < staleAfter {
		return false
	}
	if isPostFetchStatus(status) {
		return false
	}
	if live && (status == StatusRunning || status == StatusStarting) {
		return false
	}
	return status == StatusStarting || status == StatusRunning
}

// canEnterFinalizing 不含 stopped：用户停止后仍可 flush，但终态保持「已停止」，避免单站收尾写成采集完成。
func canEnterFinalizing(status string) bool {
	switch status {
	case StatusStarting, StatusRunning, StatusPageDone,
		StatusWaitingPublish, StatusDone:
		return true
	default:
		return false
	}
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
	if isPostFetchStatus(state.data.Status) {
		state.mu.Unlock()
		return true
	}
	if live && (state.data.Status == StatusRunning || state.data.Status == StatusStarting) {
		state.mu.Unlock()
		return true
	}
	if shouldMarkStale(state.data.Status, live, age, staleAfter) {
		old := state.data.Status
		name := state.data.Name
		state.data.Status = StatusFailed
		state.updated = now
		state.mu.Unlock()
		log.Printf("[Spider] 进度超时清理 source=%s status=%s age=%s -> failed",
			sourceID, old, age.Round(time.Second))
		staleNotifier(sourceID, name, old, age)
		return false
	}
	state.mu.Unlock()
	return true
}

func MarkSourcePagesFinished(sourceID string, flushAtEnd bool) {
	Update(sourceID, func(progress *model.CollectProgress) {
		if progress.Status != StatusRunning && progress.Status != StatusStarting {
			return
		}
		if flushAtEnd {
			progress.Status = StatusPageDone
			return
		}
		progress.Status = StatusWaitingPublish
	})
	FlushHotpathSideEffects(sourceID)
}

func FlushHotpathSideEffects(sourceIDs ...string) {
	if len(sourceIDs) == 0 {
		repository.FlushCollectSourceStats()
		writer.FlushCollectCacheInvalidations()
		return
	}
	repository.FlushCollectSourceStats(sourceIDs...)
	writer.FlushCollectCacheInvalidations()
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
}

func MarkStopped(sourceID string) {
	Update(sourceID, func(progress *model.CollectProgress) {
		if progress.Status == StatusStarting || progress.Status == StatusRunning {
			progress.Status = StatusStopped
		}
	})
}

// StampPageRunning 记录正在抓取的页码。终态和收尾态不可被 worker 写回 running。
func StampPageRunning(progress *model.CollectProgress, page int) {
	if IsTerminalStatus(progress.Status) || isPostFetchStatus(progress.Status) {
		return
	}
	if page > progress.Current {
		progress.Current = page
	}
	progress.Status = StatusRunning
}

func MarkSourcesFinalizing(sources map[string]model.FilmSource) {
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		ids = append(ids, source.Id)
		Update(source.Id, func(progress *model.CollectProgress) {
			if canEnterFinalizing(progress.Status) {
				progress.Status = StatusFinalizing
			}
		})
	}
	FlushHotpathSideEffects(ids...)
}

func MarkSourcesPublished(sources map[string]model.FilmSource) {
	for _, source := range sources {
		Update(source.Id, func(progress *model.CollectProgress) {
			if progress.Status == StatusFinalizing {
				progress.Status = StatusDone
			}
		})
	}
}

func MarkSourcesFinalizeFailed(sources map[string]model.FilmSource) {
	for _, source := range sources {
		Update(source.Id, func(progress *model.CollectProgress) {
			if progress.Status == StatusFinalizing {
				progress.Status = StatusFailed
			}
		})
	}
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
			if shouldMarkStale(progress.Status, live, age, staleAfter) {
				old := progress.Status
				name := progress.Name
				progress.Status = StatusFailed
				state.data.Status = StatusFailed
				state.updated = now
				log.Printf("[Spider] 进度超时清理 source=%s status=%s age=%s -> failed", id, old, age.Round(time.Second))
				state.mu.Unlock()
				staleNotifier(id, name, old, age)
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
