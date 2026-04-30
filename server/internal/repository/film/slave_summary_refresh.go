package film

import (
	"log"
	"sort"
	"strings"
	"sync"

	"server/internal/infra/db"
	"server/internal/model"
)

var slavePlaySummaryRefresh = newSlavePlaySummaryRefreshScheduler()

const slavePlaySummaryRefreshMIDChunkSize = 500

type slavePlaySummaryRefreshScheduler struct {
	mu     sync.Mutex
	states map[string]*slavePlaySummaryRefreshState
}

type slavePlaySummaryRefreshState struct {
	pending  map[int64]struct{}
	flushing bool
	waiters  []chan error
}

func newSlavePlaySummaryRefreshScheduler() *slavePlaySummaryRefreshScheduler {
	return &slavePlaySummaryRefreshScheduler{states: make(map[string]*slavePlaySummaryRefreshState)}
}

func ScheduleSlavePlaySummaryRefresh(sourceID string, infos ...model.FilmIndex) {
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

	slavePlaySummaryRefresh.schedule(sourceID, midSet)
}

func FlushPendingSlavePlaySummaryRefresh(sourceID string) error {
	return slavePlaySummaryRefresh.flush(sourceID)
}

func (s *slavePlaySummaryRefreshScheduler) schedule(sourceID string, midSet map[int64]struct{}) {
	s.mu.Lock()
	state := s.getOrCreateStateLocked(sourceID)
	for mid := range midSet {
		state.pending[mid] = struct{}{}
	}
	s.mu.Unlock()
}

func (s *slavePlaySummaryRefreshScheduler) flush(sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil
	}

	for {
		s.mu.Lock()
		state := s.states[sourceID]
		if state == nil {
			s.mu.Unlock()
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
			return nil
		}
		pending := state.pending
		state.pending = make(map[int64]struct{})
		state.flushing = true
		s.mu.Unlock()

		err := flushSlavePlaySummaryRefreshSource(sourceID, pending)
		s.finishFlush(sourceID, err)
		return err
	}
}

func (s *slavePlaySummaryRefreshScheduler) finishFlush(sourceID string, err error) {
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

func (s *slavePlaySummaryRefreshScheduler) getOrCreateStateLocked(sourceID string) *slavePlaySummaryRefreshState {
	state := s.states[sourceID]
	if state != nil {
		return state
	}
	state = &slavePlaySummaryRefreshState{pending: make(map[int64]struct{})}
	s.states[sourceID] = state
	return state
}

func flushSlavePlaySummaryRefreshSource(sourceID string, midSet map[int64]struct{}) error {
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

	log.Printf("[SlavePlaySummaryRefresh] 开始刷新 source=%s, mid_count=%d", sourceID, len(mids))
	for start := 0; start < len(mids); start += slavePlaySummaryRefreshMIDChunkSize {
		end := start + slavePlaySummaryRefreshMIDChunkSize
		if end > len(mids) {
			end = len(mids)
		}
		var infos []model.FilmIndex
		if err := db.Mdb.Where("mid IN ?", mids[start:end]).Find(&infos).Error; err != nil {
			return err
		}
		if err := RefreshPlayFromSummaryByIndexes(infos); err != nil {
			return err
		}
	}
	log.Printf("[SlavePlaySummaryRefresh] 刷新完成 source=%s, mid_count=%d", sourceID, len(mids))
	return nil
}
