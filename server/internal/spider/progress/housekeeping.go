package progress

import (
	"log"
	"sync"
	"time"

	"server/internal/config"
)

var housekeepingOnce sync.Once

func retainDuration() time.Duration {
	sec := config.CollectProgressRetainSec
	if sec <= 0 {
		sec = config.DefaultCollectProgressRetainSec
	}
	return time.Duration(sec) * time.Second
}

func staleDuration() time.Duration {
	sec := config.CollectProgressStaleSec
	if sec <= 0 {
		sec = config.DefaultCollectProgressStaleSec
	}
	return time.Duration(sec) * time.Second
}

func maybePurgeTerminal(now time.Time) {
	retain := retainDuration()
	store.Range(func(key, value any) bool {
		id, _ := key.(string)
		state := value.(*progressState)
		state.mu.RLock()
		terminal := IsTerminalStatus(state.data.Status)
		updated := state.updated
		state.mu.RUnlock()
		if terminal && now.Sub(updated) >= retain {
			store.Delete(id)
		}
		return true
	})
}

func pruneStale() {
	now := time.Now()
	staleAfter := staleDuration()
	store.Range(func(key, value any) bool {
		id, _ := key.(string)
		state := value.(*progressState)
		state.mu.Lock()
		status := state.data.Status
		updated := state.updated
		age := now.Sub(updated)
		if isActiveStatus(status) {
			_, live := tasks.Load(id)
			if shouldMarkStale(status, live, age, staleAfter) {
				name := state.data.Name
				state.data.Status = StatusFailed
				state.updated = now
				state.mu.Unlock()
				log.Printf("[Spider] 进度超时清理 source=%s status=%s age=%s -> failed", id, status, age.Round(time.Second))
				staleNotifier(id, name, status, age)
				return true
			}
		}
		state.mu.Unlock()
		return true
	})
	maybePurgeTerminal(now)
}

func StartHousekeeping() {
	housekeepingOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				pruneStale()
			}
		}()
	})
}
