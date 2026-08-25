package spider

import (
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"server/internal/infra/syslog"
	"server/internal/model"
	filmrepo "server/internal/repository/film"
)

var asyncMasterSearchTagsMu sync.Mutex

func finalizeCollectRun(sources []model.FilmSource, affectedMIDs []int64, masterMIDs []int64) ([]int64, []int64, error) {
	if len(sources) == 0 {
		return affectedMIDs, masterMIDs, nil
	}
	start := time.Now()
	log.Printf("[Spider][Finalizer] 开始收尾发布 source_count=%d", len(sources))

	if err := flushMasterSideEffects(sources, masterMIDs); err != nil {
		return affectedMIDs, masterMIDs, err
	}
	playSummaryMIDs, err := flushPlaySummaryRefresh()
	affectedMIDs = append(affectedMIDs, playSummaryMIDs...)
	if err != nil {
		return affectedMIDs, masterMIDs, err
	}
	version, err := publishFilmSnapshot(affectedMIDs)
	if err != nil {
		return affectedMIDs, masterMIDs, err
	}
	log.Printf("[Spider][Finalizer] 收尾发布完成 version=%s source_count=%d cost=%s", version, len(sources), time.Since(start))
	return affectedMIDs, masterMIDs, nil
}

func flushMasterSideEffects(sources []model.FilmSource, masterMIDs []int64) error {
	for _, source := range sources {
		if source.Grade == model.MasterCollect {
			scheduleMasterSearchTagsRefresh(masterMIDs)
			filmrepo.ClearTVBoxConfigCache()
			return nil
		}
	}
	return nil
}

func scheduleMasterSearchTagsRefresh(masterMIDs []int64) {
	mids := normalizeAffectedMIDs(masterMIDs)
	if len(mids) == 0 {
		return
	}
	go func() {
		asyncMasterSearchTagsMu.Lock()
		defer asyncMasterSearchTagsMu.Unlock()

		start := time.Now()
		log.Printf("[Spider][Finalizer] 主站搜索标签异步刷新开始 mid_count=%d", len(mids))
		if err := filmrepo.RefreshSearchTagsByMids(mids...); err != nil {
			syslog.Errorf("[Spider][Finalizer] 主站搜索标签异步刷新失败 mid_count=%d err=%v", len(mids), err)
			return
		}
		filmrepo.ClearAllSearchTagsCache()
		filmrepo.ClearAdminFilmSearchCache()
		log.Printf("[Spider][Finalizer] 主站搜索标签异步刷新完成 mid_count=%d cost=%s", len(mids), time.Since(start))
	}()
}

func flushPlaySummaryRefresh() ([]int64, error) {
	start := time.Now()
	mids, err := filmrepo.FlushPendingPlaySummaryRefresh()
	if err != nil {
		return mids, fmt.Errorf("flush play summary refresh failed: %w", err)
	}
	log.Printf("[Spider][Finalizer] 播放源摘要刷新完成 mid_count=%d cost=%s", len(mids), time.Since(start))
	return mids, nil
}

func publishFilmSnapshot(affectedMIDs []int64) (string, error) {
	start := time.Now()
	mids := normalizeAffectedMIDs(affectedMIDs)
	if len(mids) == 0 {
		if hasSnapshot, err := filmrepo.HasPublishedFilmListSnapshot(); err != nil {
			return "", err
		} else if !hasSnapshot {
			log.Printf("[Spider][Finalizer] 主站快照未发布，跳过空增量快照发布 cost=%s", time.Since(start))
			return "", nil
		}
	}
	version, updated, err := filmrepo.UpsertActiveSnapshotsByMids(mids...)
	if err != nil {
		return "", fmt.Errorf("upsert film list snapshot failed: %w", err)
	}
	log.Printf("[Spider][Finalizer] 前台影片列表快照已增量发布 version=%s input=%d updated=%d cost=%s", version, len(mids), updated, time.Since(start))
	return version, nil
}

func normalizeAffectedMIDs(mids []int64) []int64 {
	if len(mids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(mids))
	res := make([]int64, 0, len(mids))
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		seen[mid] = struct{}{}
		res = append(res, mid)
	}
	sort.Slice(res, func(i, j int) bool { return res[i] < res[j] })
	return res
}
