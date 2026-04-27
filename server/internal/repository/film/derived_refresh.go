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
	flushing      bool
	lastVisibleAt time.Time
	waiters       []chan error
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
	pendingCount := len(state.pending)
	s.mu.Unlock()

	log.Printf("[DerivedRefresh] 入队 source=%s, added_pid=%d, pending_pid=%d, start_worker=%t", sourceID, len(pidSet), pendingCount, false)

	if shouldInvalidateVisibleCaches {
		invalidateDerivedVisibleCaches()
	}
}

func (s *derivedRefreshScheduler) flush(sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil
	}

	for {
		s.mu.Lock()
		state := s.states[sourceID]
		if state == nil {
			s.mu.Unlock()
			log.Printf("[DerivedRefresh] Flush跳过 source=%s, reason=no_state", sourceID)
			return nil
		}
		if state.flushing {
			ack := make(chan error, 1)
			state.waiters = append(state.waiters, ack)
			s.mu.Unlock()
			if err := <-ack; err != nil {
				return err
			}
			continue
		}
		if len(state.pending) == 0 {
			s.mu.Unlock()
			log.Printf("[DerivedRefresh] Flush跳过 source=%s, reason=no_pending", sourceID)
			return nil
		}
		pending := state.pending
		pendingCount := len(pending)
		state.pending = make(map[int64]struct{})
		state.flushing = true
		s.mu.Unlock()

		log.Printf("[DerivedRefresh] Flush请求 source=%s, pending_pid=%d, running=%t, start_worker=%t", sourceID, pendingCount, false, true)
		err := flushDerivedRefreshSource(sourceID, pending)
		s.finishFlush(sourceID, err)
		return err
	}
}

func (s *derivedRefreshScheduler) finishFlush(sourceID string, err error) {
	s.mu.Lock()
	state := s.states[sourceID]
	if state == nil {
		s.mu.Unlock()
		return
	}
	state.flushing = false
	waiters := state.waiters
	state.waiters = nil
	if len(state.pending) == 0 {
		delete(s.states, sourceID)
	}
	s.mu.Unlock()

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
	if err := RefreshSearchTagsByPids(pids...); err != nil {
		return err
	}
	log.Printf("[DerivedRefresh] 刷新完成 source=%s, pid_count=%d", sourceID, len(pids))
	return nil
}
