package progress

import (
	"log"
	"time"

	"server/internal/model"
)

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

		// 限流暂停和停止收尾都不在任务表里：暂停的 2 分钟内任务已被摘除，不能当成失联清成失败。
		if progress.Status == StatusStopping || progress.Status == StatusPaused {
			state.mu.Unlock()
			list = append(list, progress)
			return true
		}

		if isActiveStatus(progress.Status) {
			_, live := tasks.Load(id)
			status := progress.Status
			state.mu.Unlock()
			if live {
				list = append(list, progress)
				return true
			}
			occupied := abandonedOccupied(id, status, live)
			state.mu.Lock()
			progress = state.data
			_, live = tasks.Load(id)
			if isActiveStatus(progress.Status) && shouldMarkAbandoned(progress.Status, live, occupied) {
				old := progress.Status
				progress.Status = StatusFailed
				state.data.Status = StatusFailed
				state.updated = now
				log.Printf("[Spider] 进度清理 source=%s status=%s live=%t occupied=%t -> failed",
					id, old, live, occupied)
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

		if progress.Status == StatusSealing {
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
