package spider

import (
	"log"
	"sync"
	"time"

	"server/internal/config"
)

var progressHousekeepingOnce sync.Once

func progressRetainDuration() time.Duration {
	sec := config.CollectProgressRetainSec
	if sec <= 0 {
		sec = config.DefaultCollectProgressRetainSec
	}
	return time.Duration(sec) * time.Second
}

func progressStaleDuration() time.Duration {
	sec := config.CollectProgressStaleSec
	if sec <= 0 {
		sec = config.DefaultCollectProgressStaleSec
	}
	return time.Duration(sec) * time.Second
}

func hasAnyLiveOrActiveCollect() bool {
	has := false
	activeTasks.Range(func(key, value any) bool {
		has = true
		return false
	})
	if has {
		return true
	}
	collectProgress.Range(func(key, value any) bool {
		state := value.(*collectProgressState)
		state.mu.RLock()
		active := isActiveCollectStatus(state.data.Status)
		state.mu.RUnlock()
		if active {
			has = true
			return false
		}
		return true
	})
	return has
}

func latestTerminalProgressTime() (latest time.Time, ok bool) {
	collectProgress.Range(func(key, value any) bool {
		state := value.(*collectProgressState)
		state.mu.RLock()
		status := state.data.Status
		updated := state.updated
		state.mu.RUnlock()
		if !isTerminalCollectStatus(status) {
			return true
		}
		if !ok || updated.After(latest) {
			latest = updated
			ok = true
		}
		return true
	})
	return latest, ok
}

func purgeAllTerminalProgress() {
	collectProgress.Range(func(key, value any) bool {
		id, _ := key.(string)
		state := value.(*collectProgressState)
		state.mu.RLock()
		terminal := isTerminalCollectStatus(state.data.Status)
		state.mu.RUnlock()
		if terminal {
			collectProgress.Delete(id)
		}
		return true
	})
}

func maybePurgeTerminalProgressTogether(now time.Time) {
	if hasAnyLiveOrActiveCollect() {
		return
	}
	latest, ok := latestTerminalProgressTime()
	if !ok {
		return
	}
	if now.Sub(latest) < progressRetainDuration() {
		return
	}
	purgeAllTerminalProgress()
}

func pruneStaleCollectProgress() {
	now := time.Now()
	staleAfter := progressStaleDuration()
	collectProgress.Range(func(key, value any) bool {
		id, _ := key.(string)
		state := value.(*collectProgressState)
		state.mu.Lock()
		status := state.data.Status
		updated := state.updated
		age := now.Sub(updated)
		if isActiveCollectStatus(status) {
			_, live := activeTasks.Load(id)
			if shouldMarkProgressStale(status, live, age, staleAfter) {
				name := state.data.Name
				state.data.Status = progressStatusFailed
				state.updated = now
				state.mu.Unlock()
				log.Printf("[Spider] 进度超时清理 source=%s status=%s age=%s -> failed", id, status, age.Round(time.Second))
				emitProgressStaleNotify(id, name, status, age)
				return true
			}
		}
		state.mu.Unlock()
		return true
	})
	maybePurgeTerminalProgressTogether(now)
}

func startCollectProgressHousekeeping() {
	progressHousekeepingOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				pruneStaleCollectProgress()
			}
		}()
	})
}
