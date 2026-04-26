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
	pending      map[int64]struct{}
	running      bool
	flushWaiters []chan error
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
	shouldStartWorker := !state.running
	if shouldStartWorker {
		state.running = true
	}
	pendingCount := len(state.pending)
	s.mu.Unlock()

	log.Printf("[SlaveSummaryRefresh] 入队 source=%s, added_mid=%d, pending_mid=%d, start_worker=%t", sourceID, len(midSet), pendingCount, shouldStartWorker)

	if shouldStartWorker {
		go s.runSourceWorker(sourceID)
	}
}

func (s *slaveSummaryRefreshScheduler) flush(sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil
	}

	ack := make(chan error, 1)
	s.mu.Lock()
	state := s.states[sourceID]
	if state == nil {
		s.mu.Unlock()
		log.Printf("[SlaveSummaryRefresh] Flush跳过 source=%s, reason=no_state", sourceID)
		return nil
	}
	if !state.running && len(state.pending) == 0 {
		s.mu.Unlock()
		log.Printf("[SlaveSummaryRefresh] Flush跳过 source=%s, reason=no_pending", sourceID)
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

	log.Printf("[SlaveSummaryRefresh] Flush请求 source=%s, pending_mid=%d, running=%t, start_worker=%t", sourceID, pendingCount, wasRunning, shouldStartWorker)

	if shouldStartWorker {
		go s.runSourceWorker(sourceID)
	}
	return <-ack
}

func (s *slaveSummaryRefreshScheduler) runSourceWorker(sourceID string) {
	log.Printf("[SlaveSummaryRefresh] Worker启动 source=%s", sourceID)
	for {
		midSet, waiters := s.takePendingBatch(sourceID)
		if len(midSet) == 0 {
			log.Printf("[SlaveSummaryRefresh] Worker空转退出 source=%s, waiter_count=%d", sourceID, len(waiters))
			s.finishSourceWorker(sourceID, waiters, nil)
			return
		}

		if err := flushSlaveSummaryRefreshSource(sourceID, midSet); err != nil {
			s.finishSourceWorker(sourceID, waiters, err)
			return
		}
		if len(waiters) > 0 {
			s.resolveWaiters(waiters, nil)
		}
	}
}

func (s *slaveSummaryRefreshScheduler) takePendingBatch(sourceID string) (map[int64]struct{}, []chan error) {
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

func (s *slaveSummaryRefreshScheduler) finishSourceWorker(sourceID string, waiters []chan error, err error) {
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

func (s *slaveSummaryRefreshScheduler) resolveWaiters(waiters []chan error, err error) {
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
