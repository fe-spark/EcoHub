package film

import (
	"log"
	"sort"
	"strings"
	"sync"

	"server/internal/infra/db"
	"server/internal/model"
)

var slaveSummaryRefresh = newSlaveSummaryRefreshScheduler()

type slaveSummaryRefreshScheduler struct {
	mu     sync.Mutex
	states map[string]*slaveSummaryRefreshState
}

type slaveSummaryRefreshState struct {
	pending  map[int64]struct{}
	flushing bool
	waiters  []chan error
}

func newSlaveSummaryRefreshScheduler() *slaveSummaryRefreshScheduler {
	return &slaveSummaryRefreshScheduler{states: make(map[string]*slaveSummaryRefreshState)}
}

func ScheduleSlaveSummaryRefresh(sourceID string, infos ...model.SearchInfo) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || len(infos) == 0 {
		return
	}

	midSet := make(map[int64]struct{}, len(infos))
	for _, info := range infos {
		if info.Mid > 0 {
			midSet[info.Mid] = struct{}{}
		}
	}
	if len(midSet) == 0 {
		return
	}

	slaveSummaryRefresh.schedule(sourceID, midSet)
}

func FlushPendingSlaveSummaryRefresh(sourceID string) error {
	return slaveSummaryRefresh.flush(sourceID)
}

func (s *slaveSummaryRefreshScheduler) schedule(sourceID string, midSet map[int64]struct{}) {
	s.mu.Lock()
	state := s.getOrCreateStateLocked(sourceID)
	for mid := range midSet {
		state.pending[mid] = struct{}{}
	}
	pendingCount := len(state.pending)
	s.mu.Unlock()

	log.Printf("[SlaveSummaryRefresh] 入队 source=%s, added_mid=%d, pending_mid=%d, start_worker=%t", sourceID, len(midSet), pendingCount, false)
}

func (s *slaveSummaryRefreshScheduler) flush(sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil
	}

	for {
		s.mu.Lock()
		state := s.states[sourceID]
		if state == nil {
			s.mu.Unlock()
			log.Printf("[SlaveSummaryRefresh] Flush跳过 source=%s, reason=no_state", sourceID)
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
			log.Printf("[SlaveSummaryRefresh] Flush跳过 source=%s, reason=no_pending", sourceID)
			return nil
		}
		pending := state.pending
		pendingCount := len(pending)
		state.pending = make(map[int64]struct{})
		state.flushing = true
		s.mu.Unlock()

		log.Printf("[SlaveSummaryRefresh] Flush请求 source=%s, pending_mid=%d, running=%t, start_worker=%t", sourceID, pendingCount, false, true)
		err := flushSlaveSummaryRefreshSource(sourceID, pending)
		s.finishFlush(sourceID, err)
		return err
	}
}

func (s *slaveSummaryRefreshScheduler) finishFlush(sourceID string, err error) {
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

func (s *slaveSummaryRefreshScheduler) getOrCreateStateLocked(sourceID string) *slaveSummaryRefreshState {
	state := s.states[sourceID]
	if state != nil {
		return state
	}
	state = &slaveSummaryRefreshState{pending: make(map[int64]struct{})}
	s.states[sourceID] = state
	return state
}

func flushSlaveSummaryRefreshSource(sourceID string, midSet map[int64]struct{}) error {
	if len(midSet) == 0 {
		return nil
	}

	mids := make([]int64, 0, len(midSet))
	for mid := range midSet {
		mids = append(mids, mid)
	}
	sort.Slice(mids, func(i, j int) bool {
		return mids[i] < mids[j]
	})

	var infos []model.SearchInfo
	if err := db.Mdb.Where("mid IN ?", mids).Find(&infos).Error; err != nil {
		return err
	}

	log.Printf("[SlaveSummaryRefresh] 开始刷新 source=%s, mid_count=%d", sourceID, len(mids))
	if err := RefreshPlayFromSummaryBySearchInfos(infos); err != nil {
		return err
	}
	log.Printf("[SlaveSummaryRefresh] 刷新完成 source=%s, mid_count=%d", sourceID, len(mids))
	return nil
}
