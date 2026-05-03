package film

import (
	"log"
	"sort"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
)

var playSummaryRefresh = newPlaySummaryRefreshScheduler()

const playSummaryRefreshMIDChunkSize = 500

type playSummaryRefreshScheduler struct {
	mu       sync.Mutex
	pending  map[int64]struct{}
	flushing bool
	waiters  []chan error
}

func newPlaySummaryRefreshScheduler() *playSummaryRefreshScheduler {
	return &playSummaryRefreshScheduler{pending: make(map[int64]struct{})}
}

func SchedulePlaySummaryRefresh(infos ...model.FilmIndex) {
	if len(infos) == 0 {
		return
	}

	playSummaryRefresh.mu.Lock()
	for _, info := range infos {
		if info.Mid > 0 {
			playSummaryRefresh.pending[info.Mid] = struct{}{}
		}
	}
	playSummaryRefresh.mu.Unlock()
}

func FlushPendingPlaySummaryRefresh() error {
	return playSummaryRefresh.flush()
}

func (s *playSummaryRefreshScheduler) flush() error {
	for {
		s.mu.Lock()
		if s.flushing {
			ack := make(chan error, 1)
			s.waiters = append(s.waiters, ack)
			s.mu.Unlock()
			if err := <-ack; err != nil {
				return err
			}
			continue
		}
		if len(s.pending) == 0 {
			s.mu.Unlock()
			return nil
		}
		pending := s.pending
		s.pending = make(map[int64]struct{})
		s.flushing = true
		s.mu.Unlock()

		err := flushPlaySummaryRefreshMids(pending)
		s.finishFlush(err)
		return err
	}
}

func (s *playSummaryRefreshScheduler) finishFlush(err error) {
	s.mu.Lock()
	s.flushing = false
	waiters := s.waiters
	s.waiters = nil
	s.mu.Unlock()

	for _, waiter := range waiters {
		waiter <- err
	}
}

func flushPlaySummaryRefreshMids(midSet map[int64]struct{}) error {
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

	startedAt := time.Now()
	log.Printf("[PlaySummaryRefresh] 开始刷新 mid_count=%d", len(mids))
	for start := 0; start < len(mids); start += playSummaryRefreshMIDChunkSize {
		chunkStartedAt := time.Now()
		end := start + playSummaryRefreshMIDChunkSize
		if end > len(mids) {
			end = len(mids)
		}
		var infos []model.FilmIndex
		if err := db.Mdb.Where("mid IN ?", mids[start:end]).Find(&infos).Error; err != nil {
			return err
		}
		if err := RefreshPlayFromSummaryByIndexesTx(db.Mdb, infos); err != nil {
			return err
		}
		log.Printf(
			"[PlaySummaryRefresh] 刷新进度 mid=%d/%d chunk=%d cost=%s total_cost=%s",
			end,
			len(mids),
			end-start,
			time.Since(chunkStartedAt),
			time.Since(startedAt),
		)
	}
	log.Printf("[PlaySummaryRefresh] 刷新完成 mid_count=%d cost=%s", len(mids), time.Since(startedAt))
	return nil
}
