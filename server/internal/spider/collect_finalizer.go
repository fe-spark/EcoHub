package spider

import (
	"fmt"
	"log"
	"sort"
	"time"

	filmsnapshot "server/internal/repository/film/snapshot"
)

func publishFilmSnapshot(affectedMIDs []int64) (string, error) {
	start := time.Now()
	mids := normalizeAffectedMIDs(affectedMIDs)
	if len(mids) == 0 {
		if hasSnapshot, err := filmsnapshot.HasPublishedFilmListSnapshot(); err != nil {
			return "", err
		} else if !hasSnapshot {
			log.Printf("[Spider][Finalizer] 快照未发布，跳过空增量快照发布 cost=%s", time.Since(start))
			return "", nil
		}
	}
	version, updated, err := filmsnapshot.UpsertActiveSnapshotsByMids(mids...)
	if err != nil {
		return "", fmt.Errorf("upsert film list snapshot failed: %w", err)
	}
	log.Printf("[Spider][Finalizer] 列表可见性已刷新 version=%s input=%d updated=%d cost=%s", version, len(mids), updated, time.Since(start))
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
