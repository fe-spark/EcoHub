package snapshot

import (
	"log"
	"sort"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
)

var playSummaryRefresh = newPlaySummaryRefreshScheduler()

const playSummaryRefreshMIDChunkSize = 200

type playSummaryRefreshScheduler struct {
	mu      sync.Mutex
	pending map[int64]struct{}
}

func newPlaySummaryRefreshScheduler() *playSummaryRefreshScheduler {
	return &playSummaryRefreshScheduler{pending: make(map[int64]struct{})}
}

// FlushPlaySummaryRefreshByMids 只刷新本批 affected mids，不排空其它采集队列积压的 pending。
func FlushPlaySummaryRefreshByMids(mids []int64) ([]int64, error) {
	selected := takePendingPlaySummaryMids(mids)
	if len(selected) == 0 {
		return nil, nil
	}
	err := flushPlaySummaryRefreshMids(selected)
	return sortedMIDsFromSet(selected), err
}

func takePendingPlaySummaryMids(mids []int64) map[int64]struct{} {
	if len(mids) == 0 {
		return nil
	}
	playSummaryRefresh.mu.Lock()
	defer playSummaryRefresh.mu.Unlock()
	selected := make(map[int64]struct{}, len(mids))
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if _, ok := playSummaryRefresh.pending[mid]; !ok {
			continue
		}
		selected[mid] = struct{}{}
		delete(playSummaryRefresh.pending, mid)
	}
	if len(selected) == 0 {
		return nil
	}
	return selected
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
