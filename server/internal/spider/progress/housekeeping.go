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

func maybePurgeTerminal(now time.Time) bool {
	retain := retainDuration()
	activeQueues := make(map[string]struct{})
	store.Range(func(_, value any) bool {
		state := value.(*progressState)
		state.mu.RLock()
		status := state.data.Status
		queueID := state.data.QueueId
		state.mu.RUnlock()
		if queueID != "" && !IsTerminalStatus(status) {
			activeQueues[queueID] = struct{}{}
		}
		return true
	})
	var purged bool
	store.Range(func(key, value any) bool {
		id, _ := key.(string)
		state := value.(*progressState)
		state.mu.RLock()
		terminal := IsTerminalStatus(state.data.Status)
		updated := state.updated
		queueID := state.data.QueueId
		state.mu.RUnlock()
		if !terminal || now.Sub(updated) < retain {
			return true
		}
		// 队列还没结束时保留终态成员，停止/失败的站点不会提前从队列里消失。
		if queueID != "" {
			if _, active := activeQueues[queueID]; active {
				return true
			}
		}
		store.Delete(id)
		purged = true
		return true
	})
	return purged
}

func pruneStale() {
	now := time.Now()
	staleAfter := staleDuration()
	var changed bool
	store.Range(func(key, value any) bool {
		id, _ := key.(string)
		state := value.(*progressState)
		state.mu.Lock()
		status := state.data.Status
		updated := state.updated
		age := now.Sub(updated)
		if isActiveStatus(status) {
			_, live := tasks.Load(id)
			if shouldMarkStale(id, status, live, age, staleAfter) {
				state.data.Status = StatusFailed
				state.updated = now
				state.mu.Unlock()
				changed = true
				log.Printf("[Spider] 进度超时清理 source=%s status=%s age=%s -> failed", id, status, age.Round(time.Second))
				return true
			}
		}
		// 封口周期丢失（进程重启等）时把等检索的站点收成最终状态，避免状态悬着。
		if status == StatusSealing && age >= staleAfter {
			if state.sealOutcome == StatusStopped {
				state.data.Status = StatusStopped
			} else {
				state.data.Status = StatusDone
			}
			state.data.Sealed = true
			state.sealOutcome = ""
			state.updated = now
			state.mu.Unlock()
			changed = true
			log.Printf("[Spider] 待检索超时收尾 source=%s age=%s", id, age.Round(time.Second))
			return true
		}
		state.mu.Unlock()
		return true
	})
	if maybePurgeTerminal(now) {
		changed = true
	}
	if changed {
		NotifyChange()
	}
}

func StartHousekeeping() {
	housekeepingOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				pruneStale()
			}
		}()
	})
}
