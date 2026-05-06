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
	waiters  []chan playSummaryRefreshResult
}

type playSummaryRefreshResult struct {
	mids []int64
	err  error
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

func FlushPendingPlaySummaryRefresh() ([]int64, error) {
	return playSummaryRefresh.flush()
}

func (s *playSummaryRefreshScheduler) flush() ([]int64, error) {
	flushedMIDs := make([]int64, 0)
	for {
		s.mu.Lock()
		if s.flushing {
			ack := make(chan playSummaryRefreshResult, 1)
			s.waiters = append(s.waiters, ack)
			s.mu.Unlock()
			result := <-ack
			flushedMIDs = append(flushedMIDs, result.mids...)
			if result.err != nil {
				return normalizePlaySummaryMIDs(flushedMIDs), result.err
			}
			continue
		}
		if len(s.pending) == 0 {
			s.mu.Unlock()
			return normalizePlaySummaryMIDs(flushedMIDs), nil
		}
		pending := s.pending
		s.pending = make(map[int64]struct{})
		s.flushing = true
		s.mu.Unlock()

		err := flushPlaySummaryRefreshMids(pending)
		mids := sortedMIDsFromSet(pending)
		s.finishFlush(playSummaryRefreshResult{mids: mids, err: err})
		flushedMIDs = append(flushedMIDs, mids...)
		return normalizePlaySummaryMIDs(flushedMIDs), err
	}
}

func (s *playSummaryRefreshScheduler) finishFlush(result playSummaryRefreshResult) {
	s.mu.Lock()
	s.flushing = false
	waiters := s.waiters
	s.waiters = nil
	s.mu.Unlock()

	for _, waiter := range waiters {
		waiter <- result
	}
}

func flushPlaySummaryRefreshMids(midSet map[int64]struct{}) error {
	if len(midSet) == 0 {
		return nil
	}

	mids := sortedMIDsFromSet(midSet)

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

func sortedMIDsFromSet(midSet map[int64]struct{}) []int64 {
	mids := make([]int64, 0, len(midSet))
	for mid := range midSet {
		if mid > 0 {
			mids = append(mids, mid)
		}
	}
	sort.Slice(mids, func(i, j int) bool {
		return mids[i] < mids[j]
	})
	return mids
}

func normalizePlaySummaryMIDs(mids []int64) []int64 {
	if len(mids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(mids))
	midSet := make(map[int64]struct{}, len(mids))
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		seen[mid] = struct{}{}
		midSet[mid] = struct{}{}
	}
	return sortedMIDsFromSet(midSet)
}
