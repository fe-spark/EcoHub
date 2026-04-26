package film

import (
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"
)

const derivedVisibleCacheInvalidateInterval = 2 * time.Second

var derivedRefresh = newDerivedRefreshScheduler()

type derivedRefreshScheduler struct {
	mu     sync.Mutex
	states map[string]*derivedRefreshState
}

type derivedRefreshState struct {
	pending       map[int64]struct{}
	running       bool
	lastVisibleAt time.Time
	flushWaiters  []chan error
}

func newDerivedRefreshScheduler() *derivedRefreshScheduler {
	return &derivedRefreshScheduler{states: make(map[string]*derivedRefreshState)}
}

func ScheduleDerivedRefresh(sourceID string, infos ...model.SearchInfo) {
	derivedRefresh.schedule(sourceID, collectSearchTagPids(infos))
}

func FlushPendingDerivedRefresh(sourceID string) error {
	return derivedRefresh.flush(sourceID)
}

func (s *derivedRefreshScheduler) schedule(sourceID string, pidSet map[int64]bool) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || len(pidSet) == 0 {
		return
	}

	now := time.Now()
	s.mu.Lock()
	state := s.getOrCreateStateLocked(sourceID)
	for pid := range pidSet {
		if pid > 0 {
			state.pending[pid] = struct{}{}
		}
	}
	shouldInvalidateVisibleCaches := shouldInvalidateDerivedVisibleCaches(state.lastVisibleAt, now)
	if shouldInvalidateVisibleCaches {
		state.lastVisibleAt = now
	}
	shouldStartWorker := !state.running
	if shouldStartWorker {
		state.running = true
	}
	pendingCount := len(state.pending)
	s.mu.Unlock()

	log.Printf("[DerivedRefresh] 入队 source=%s, added_pid=%d, pending_pid=%d, start_worker=%t", sourceID, len(pidSet), pendingCount, shouldStartWorker)

	if shouldInvalidateVisibleCaches {
		invalidateDerivedVisibleCaches()
	}
	if shouldStartWorker {
		go s.runSourceWorker(sourceID)
	}
}

func (s *derivedRefreshScheduler) flush(sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil
	}

	ack := make(chan error, 1)
	s.mu.Lock()
	state := s.states[sourceID]
	if state == nil {
		s.mu.Unlock()
		log.Printf("[DerivedRefresh] Flush跳过 source=%s, reason=no_state", sourceID)
		return nil
	}
	if !state.running && len(state.pending) == 0 {
		s.mu.Unlock()
		log.Printf("[DerivedRefresh] Flush跳过 source=%s, reason=no_pending", sourceID)
		return nil
	}
	pendingCount := len(state.pending)
	wasRunning := state.running
	state.flushWaiters = append(state.flushWaiters, ack)
	shouldStartWorker := !state.running && len(state.pending) > 0
	if shouldStartWorker {
		state.running = true
	}
	s.mu.Unlock()

	log.Printf("[DerivedRefresh] Flush请求 source=%s, pending_pid=%d, running=%t, start_worker=%t", sourceID, pendingCount, wasRunning, shouldStartWorker)

	if shouldStartWorker {
		go s.runSourceWorker(sourceID)
	}
	return <-ack
}

func (s *derivedRefreshScheduler) runSourceWorker(sourceID string) {
	log.Printf("[DerivedRefresh] Worker启动 source=%s", sourceID)
	for {
		pidSet, waiters := s.takePendingBatch(sourceID)
		if len(pidSet) == 0 {
			log.Printf("[DerivedRefresh] Worker空转退出 source=%s, waiter_count=%d", sourceID, len(waiters))
			s.finishSourceWorker(sourceID, waiters, nil)
			return
		}

		if err := flushDerivedRefreshSource(sourceID, pidSet); err != nil {
			s.finishSourceWorker(sourceID, waiters, err)
			return
		}
		if len(waiters) > 0 {
			s.resolveWaiters(waiters, nil)
		}
	}
}

func (s *derivedRefreshScheduler) takePendingBatch(sourceID string) (map[int64]struct{}, []chan error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.states[sourceID]
	if state == nil {
		return nil, nil
	}
	if len(state.pending) == 0 {
		waiters := state.flushWaiters
		state.flushWaiters = nil
		return nil, waiters
	}

	pending := state.pending
	state.pending = make(map[int64]struct{})
	return pending, nil
}

func (s *derivedRefreshScheduler) finishSourceWorker(sourceID string, waiters []chan error, err error) {
	s.mu.Lock()
	state := s.states[sourceID]
	if state == nil {
		s.mu.Unlock()
		s.resolveWaiters(waiters, err)
		return
	}
	state.running = false
	if err != nil {
		waiters = append(waiters, state.flushWaiters...)
		state.flushWaiters = nil
	}
	shouldRestart := err == nil && len(state.pending) > 0
	if shouldRestart {
		state.running = true
	}
	if !state.running && len(state.pending) == 0 && len(state.flushWaiters) == 0 {
		delete(s.states, sourceID)
	}
	s.mu.Unlock()

	s.resolveWaiters(waiters, err)
	if shouldRestart {
		go s.runSourceWorker(sourceID)
	}
}

func (s *derivedRefreshScheduler) resolveWaiters(waiters []chan error, err error) {
	for _, waiter := range waiters {
		waiter <- err
	}
}

func (s *derivedRefreshScheduler) getOrCreateStateLocked(sourceID string) *derivedRefreshState {
	state := s.states[sourceID]
	if state != nil {
		return state
	}
	state = &derivedRefreshState{pending: make(map[int64]struct{})}
	s.states[sourceID] = state
	return state
}

func shouldInvalidateDerivedVisibleCaches(last, now time.Time) bool {
	if last.IsZero() {
		return true
	}
	return now.Sub(last) >= derivedVisibleCacheInvalidateInterval
}

func invalidateDerivedVisibleCaches() {
	db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)
	support.ClearIndexPageCache()
}

func flushDerivedRefreshSource(sourceID string, pidSet map[int64]struct{}) error {
	if len(pidSet) == 0 {
		return nil
	}

	pids := make([]int64, 0, len(pidSet))
	for pid := range pidSet {
		pids = append(pids, pid)
	}
	sort.Slice(pids, func(i, j int) bool {
		return pids[i] < pids[j]
	})

	log.Printf("[DerivedRefresh] 开始刷新 source=%s, pid_count=%d", sourceID, len(pids))
	clearSearchInfoCachesByPidSet(pidSet)
	if err := RebuildSearchTagsByPids(pids...); err != nil {
		return err
	}
	log.Printf("[DerivedRefresh] 刷新完成 source=%s, pid_count=%d", sourceID, len(pids))
	return nil
}
