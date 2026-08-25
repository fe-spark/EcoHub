package spider

import (
	"fmt"
	"log"
	"sync"
	"time"

	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
)

var collectProgress sync.Map

type collectProgressState struct {
	mu      sync.RWMutex
	data    model.CollectProgress
	updated time.Time
}

func ensureCollectProgress(sourceID string, name string) *collectProgressState {
	if val, ok := collectProgress.Load(sourceID); ok {
		state := val.(*collectProgressState)
		state.mu.Lock()
		state.data.Id = sourceID
		if name != "" {
			state.data.Name = name
		}
		state.updated = time.Now()
		state.mu.Unlock()
		return state
	}
	state := &collectProgressState{data: model.CollectProgress{Id: sourceID, Name: name, Status: progressStatusStarting}, updated: time.Now()}
	actual, _ := collectProgress.LoadOrStore(sourceID, state)
	return actual.(*collectProgressState)
}

func updateCollectProgress(sourceID string, update func(*model.CollectProgress)) {
	if val, ok := collectProgress.Load(sourceID); ok {
		state := val.(*collectProgressState)
		state.mu.Lock()
		update(&state.data)
		state.updated = time.Now()
		state.mu.Unlock()
	}
}

func collectProgressSnapshot(sourceID string) (model.CollectProgress, bool) {
	if val, ok := collectProgress.Load(sourceID); ok {
		state := val.(*collectProgressState)
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
	progressStatusStarting       = "starting"
	progressStatusRunning        = "running"
	progressStatusPageDone       = "page_done"
	progressStatusWaitingPublish = "waiting_publish"
	progressStatusFinalizing     = "finalizing"
	progressStatusDone           = "done"
	progressStatusFailed         = "failed"
	progressStatusStopped        = "stopped"
)

func isActiveCollectStatus(status string) bool {
	switch status {
	case progressStatusStarting, progressStatusRunning, progressStatusPageDone,
		progressStatusWaitingPublish, progressStatusFinalizing:
		return true
	default:
		return false
	}
}

func isTerminalCollectStatus(status string) bool {
	switch status {
	case progressStatusDone, progressStatusFailed, progressStatusStopped:
		return true
	default:
		return false
	}
}

func isPostFetchCollectStatus(status string) bool {
	switch status {
	case progressStatusPageDone, progressStatusWaitingPublish, progressStatusFinalizing:
		return true
	default:
		return false
	}
}

func shouldMarkProgressStale(status string, live bool, age, staleAfter time.Duration) bool {
	if age < staleAfter {
		return false
	}
	if isPostFetchCollectStatus(status) {
		return false
	}
	if live && (status == progressStatusRunning || status == progressStatusStarting) {
		return false
	}
	return status == progressStatusStarting || status == progressStatusRunning
}

func canEnterFinalizing(status string) bool {
	switch status {
	case progressStatusStarting, progressStatusRunning, progressStatusPageDone,
		progressStatusWaitingPublish, progressStatusStopped, progressStatusDone:
		return true
	default:
		return false
	}
}

func isCollectProgressStopped(sourceID string) bool {
	if progress, ok := collectProgressSnapshot(sourceID); ok {
		return progress.Status == progressStatusStopped
	}
	return false
}

func isCollectProgressStarting(sourceID string) bool {
	if progress, ok := collectProgressSnapshot(sourceID); ok {
		return progress.Status == progressStatusStarting
	}
	return false
}

func isCollectAlreadyQueuedOrRunning(sourceID string) bool {
	if _, ok := activeTasks.Load(sourceID); ok {
		return true
	}
	if refreshAndIsBlockingSourceProgress(sourceID) {
		return true
	}
	return false
}

func refreshAndIsBlockingSourceProgress(sourceID string) bool {
	val, ok := collectProgress.Load(sourceID)
	if !ok {
		return false
	}
	state := val.(*collectProgressState)
	now := time.Now()
	staleAfter := progressStaleDuration()

	state.mu.Lock()
	if !isActiveCollectStatus(state.data.Status) {
		state.mu.Unlock()
		return false
	}
	_, live := activeTasks.Load(sourceID)
	age := now.Sub(state.updated)
	if isPostFetchCollectStatus(state.data.Status) {
		state.mu.Unlock()
		return true
	}
	if live && (state.data.Status == progressStatusRunning || state.data.Status == progressStatusStarting) {
		state.mu.Unlock()
		return true
	}
	if shouldMarkProgressStale(state.data.Status, live, age, staleAfter) {
		old := state.data.Status
		name := state.data.Name
		state.data.Status = progressStatusFailed
		state.updated = now
		state.mu.Unlock()
		log.Printf("[Spider] 进度超时清理 source=%s status=%s age=%s -> failed",
			sourceID, old, age.Round(time.Second))
		emitProgressStaleNotify(sourceID, name, old, age)
		return false
	}
	state.mu.Unlock()
	return true
}

func markSourcePagesFinished(sourceID string, flushAtEnd bool) {
	updateCollectProgress(sourceID, func(progress *model.CollectProgress) {
		if progress.Status != progressStatusRunning && progress.Status != progressStatusStarting {
			return
		}
		if flushAtEnd {
			progress.Status = progressStatusPageDone
			return
		}
		progress.Status = progressStatusWaitingPublish
	})
	flushCollectHotpathSideEffects(sourceID)
}

func flushCollectHotpathSideEffects(sourceIDs ...string) {
	if len(sourceIDs) == 0 {
		repository.FlushCollectSourceStats()
		filmrepo.FlushCollectCacheInvalidations()
		return
	}
	repository.FlushCollectSourceStats(sourceIDs...)
	filmrepo.FlushCollectCacheInvalidations()
}

func filterCollectableSources(sources []model.FilmSource, tag string) []model.FilmSource {
	filtered := make([]model.FilmSource, 0, len(sources))
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if _, ok := seen[source.Id]; ok {
			log.Printf("[%s] 站点 %s 在本轮采集列表中重复，跳过", tag, source.Name)
			continue
		}
		seen[source.Id] = struct{}{}
		if isCollectAlreadyQueuedOrRunning(source.Id) {
			log.Printf("[%s] 站点 %s 已在采集队列或正在运行，跳过", tag, source.Name)
			continue
		}
		filtered = append(filtered, source)
	}
	return filtered
}

func markSourcesCollectStarting(sources []model.FilmSource) {
	for _, source := range sources {
		state := ensureCollectProgress(source.Id, source.Name)
		state.mu.Lock()
		state.data.Total = 0
		state.data.Current = 0
		state.data.Success = 0
		state.data.Failed = 0
		state.data.Status = progressStatusStarting
		state.updated = time.Now()
		state.mu.Unlock()
	}
}

func markProgressStopped(sourceID string) {
	updateCollectProgress(sourceID, func(progress *model.CollectProgress) {
		if progress.Status == progressStatusStarting || progress.Status == progressStatusRunning {
			progress.Status = progressStatusStopped
		}
	})
}

func StopTask(sourceID string) {
	markProgressStopped(sourceID)
	if val, ok := activeTasks.Load(sourceID); ok {
		val.(collectTask).cancel()
	}
}

func PrepareSingleCollectStart(source model.FilmSource) error {
	if isCollectAlreadyQueuedOrRunning(source.Id) {
		return fmt.Errorf("站点 %s 已在采集队列或正在运行，已跳过本次采集", source.Name)
	}
	markSourcesCollectStarting([]model.FilmSource{source})
	return nil
}

func markSourcesFinalizing(sources map[string]model.FilmSource) {
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		ids = append(ids, source.Id)
		updateCollectProgress(source.Id, func(progress *model.CollectProgress) {
			if canEnterFinalizing(progress.Status) {
				progress.Status = progressStatusFinalizing
			}
		})
	}
	flushCollectHotpathSideEffects(ids...)
}

func markSourcesPublished(sources map[string]model.FilmSource) {
	for _, source := range sources {
		updateCollectProgress(source.Id, func(progress *model.CollectProgress) {
			if progress.Status == progressStatusFinalizing {
				progress.Status = progressStatusDone
			}
		})
	}
}

func markSourcesFinalizeFailed(sources map[string]model.FilmSource) {
	for _, source := range sources {
		updateCollectProgress(source.Id, func(progress *model.CollectProgress) {
			if progress.Status == progressStatusFinalizing {
				progress.Status = progressStatusFailed
			}
		})
	}
}

func GetActiveTaskProgress() []model.CollectProgress {
	startCollectProgressHousekeeping()

	list := make([]model.CollectProgress, 0)
	seen := make(map[string]struct{})
	staleAfter := progressStaleDuration()
	now := time.Now()

	activeTasks.Range(func(key, value any) bool {
		id := key.(string)
		seen[id] = struct{}{}
		if progress, ok := collectProgressSnapshot(id); ok {
			list = append(list, progress)
			return true
		}
		list = append(list, model.CollectProgress{Id: id, Status: progressStatusRunning})
		return true
	})
	collectProgress.Range(func(key, value any) bool {
		id := key.(string)
		if _, ok := seen[id]; ok {
			return true
		}
		state := value.(*collectProgressState)
		state.mu.Lock()
		progress := state.data
		age := now.Sub(state.updated)

		if isActiveCollectStatus(progress.Status) {
			_, live := activeTasks.Load(id)
			if shouldMarkProgressStale(progress.Status, live, age, staleAfter) {
				old := progress.Status
				name := progress.Name
				progress.Status = progressStatusFailed
				state.data.Status = progressStatusFailed
				state.updated = now
				log.Printf("[Spider] 进度超时清理 source=%s status=%s age=%s -> failed", id, old, age.Round(time.Second))
				state.mu.Unlock()
				emitProgressStaleNotify(id, name, old, age)
				list = append(list, progress)
				return true
			}
			state.mu.Unlock()
			list = append(list, progress)
			return true
		}

		if isTerminalCollectStatus(progress.Status) {
			state.mu.Unlock()
			list = append(list, progress)
			return true
		}
		state.mu.Unlock()
		return true
	})

	maybePurgeTerminalProgressTogether(now)
	if n := len(list); n > 0 {
		filtered := list[:0]
		for _, p := range list {
			if isTerminalCollectStatus(p.Status) {
				if _, still := collectProgress.Load(p.Id); !still {
					continue
				}
			}
			filtered = append(filtered, p)
		}
		list = filtered
	}

	return list
}

func StopAllTasks() {
	stopAllVersion.Add(1)
	count := 0
	collectProgress.Range(func(key, value any) bool {
		state := value.(*collectProgressState)
		state.mu.Lock()
		if state.data.Status == progressStatusStarting || state.data.Status == progressStatusRunning {
			state.data.Status = progressStatusStopped
			state.updated = time.Now()
		}
		state.mu.Unlock()
		return true
	})
	activeTasks.Range(func(key, value any) bool {
		if ct, ok := value.(collectTask); ok {
			ct.cancel()
			count++
		}
		if id, ok := key.(string); ok {
			markProgressStopped(id)
		}
		return true
	})
	if count > 0 {
		log.Printf("[Spider] 已强制停止 %d 个活跃采集任务\n", count)
		go finalizeStoppedCollectTasks()
	}
}

func finalizeStoppedCollectTasks() {
	if err := collectLifecycle.flushPending(); err != nil {
		syslog.Errorf("[Spider] 终止采集后收尾刷新失败: %v", err)
	}
}

func IsTaskRunning(id string) bool {
	if _, ok := activeTasks.Load(id); ok {
		return true
	}
	if progress, ok := collectProgressSnapshot(id); ok {
		return isActiveCollectStatus(progress.Status)
	}
	return false
}

func IsAnyTaskRunning() bool {
	found := false
	activeTasks.Range(func(key, value any) bool {
		found = true
		return false
	})
	if found {
		return true
	}
	hasActive := false
	collectProgress.Range(func(key, value any) bool {
		state := value.(*collectProgressState)
		state.mu.RLock()
		active := isActiveCollectStatus(state.data.Status)
		state.mu.RUnlock()
		if active {
			hasActive = true
			return false
		}
		return true
	})
	return hasActive
}
