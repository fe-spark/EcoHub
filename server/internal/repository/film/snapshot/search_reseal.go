package snapshot

import (
	"log"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
)

// ResealSearchIndexMids 只刷新这次变更的影片，再按内存里的全表重算排行和倒排。
// 没有现成索引时才退回全库扫描。采集结束走这里，避免改几百部片却把几十万行再读进内存。
func ResealSearchIndexMids(mids []int64) {
	mids = NormalizeSnapshotMIDs(mids)
	if len(mids) == 0 || db.Mdb == nil {
		return
	}

	activeFilmSearchMetasMu.Lock()
	cur := activeFilmSearchMetas.Load()
	version := currentSearchIndexVersion()
	const maxIncrementalMids = 1000
	if cur == nil || cur.Version == "" || (version != "" && cur.Version != version) || len(mids) > maxIncrementalMids {
		activeFilmSearchMetasMu.Unlock()
		log.Printf("[ActiveReadModel] 变更影片数量(%d)过多或无增量副本，触发全量重建并广播进度", len(mids))
		RequestActiveFilmSearchIndexRebuild()
		return
	}
	defer activeFilmSearchMetasMu.Unlock()
	version = cur.Version

	started := time.Now()
	upsertMap, drop, err := loadSearchMetaUpserts(mids)
	if err != nil {
		log.Printf("[ActiveReadModel] 检索索引增量读取失败 mids=%d err=%v", len(mids), err)
		return
	}
	newItems := patchSearchMetas(cur.Items, upsertMap, drop)
	idx := sealSearchMetaIndex(version, newItems)
	activeFilmSearchMetas.Store(idx)
	log.Printf("[ActiveReadModel] 检索索引按变更封口 mids=%d total=%d cost=%s",
		len(mids), len(idx.Items), time.Since(started).Round(time.Millisecond))
}

func loadSearchMetaUpserts(mids []int64) (map[int64]FilmSearchMeta, map[int64]struct{}, error) {
	drop := make(map[int64]struct{}, len(mids))
	for _, id := range mids {
		drop[id] = struct{}{}
	}
	const batchSize = 500
	var allRows []dbMetaRow
	for i := 0; i < len(mids); i += batchSize {
		end := i + batchSize
		if end > len(mids) {
			end = len(mids)
		}
		var batchRows []dbMetaRow
		if err := db.Mdb.Model(&model.FilmIndex{}).
			Select(filmMetaSelect).
			Where("mid IN ?", mids[i:end]).
			Find(&batchRows).Error; err != nil {
			return nil, nil, err
		}
		allRows = append(allRows, batchRows...)
	}
	plays, err := loadPlaySources(mids)
	if err != nil {
		return nil, nil, err
	}
	upsertMap := make(map[int64]FilmSearchMeta, len(allRows))
	for _, r := range allRows {
		meta := metaFromRow(r)
		meta.PlaySources = plays[r.Mid]
		upsertMap[r.Mid] = meta
		delete(drop, r.Mid)
	}
	return upsertMap, drop, nil
}

// patchSearchMetas 用变更覆盖旧条目，丢掉库里已经不存在的 mid，其余原样保留。
func patchSearchMetas(items []FilmSearchMeta, upsert map[int64]FilmSearchMeta, drop map[int64]struct{}) []FilmSearchMeta {
	out := make([]FilmSearchMeta, 0, len(items)+len(upsert))
	seen := make(map[int64]struct{}, len(upsert))
	for _, it := range items {
		if updated, ok := upsert[it.Mid]; ok {
			out = append(out, updated)
			seen[it.Mid] = struct{}{}
			continue
		}
		if _, gone := drop[it.Mid]; gone {
			continue
		}
		out = append(out, it)
	}
	for mid, it := range upsert {
		if _, ok := seen[mid]; !ok {
			out = append(out, it)
		}
	}
	return out
}
