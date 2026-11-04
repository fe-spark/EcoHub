package snapshot

import (
	"log"

	"server/internal/infra/db"
	"server/internal/model"
)

// searchResealSealMin 达到该变动量时，收尾在内存里重算一次倒排和榜单，不扫全库。
const searchResealSealMin = 50000

// ResealSearchIndexMids 一条采集队列的变动只封口一次。
// 库里仍在的影片走增量；变动达到 searchResealSealMin 或有删除时，用内存里的全表重算倒排和榜单。
// 没有可用内存索引时才请求全库重建。
func ResealSearchIndexMids(mids []int64) bool {
	mids = NormalizeSnapshotMIDs(mids)
	if len(mids) == 0 || db.Mdb == nil {
		return false
	}

	version := currentSearchIndexVersion()
	base := activeFilmSearchMetas.Load()
	if !searchIndexReady(base, version) {
		log.Printf("[ActiveReadModel] 内存无可用检索副本，触发全量重建 mids=%d", len(mids))
		RequestActiveFilmSearchIndexRebuild()
		return false
	}

	total := int64(len(mids))
	NotifySearchIndexProgress(total, 0, "indexing")
	upsertMap, drop, err := readSearchMetaUpserts(mids, resealLoadProgress(total))
	if err != nil {
		log.Printf("[ActiveReadModel] 检索索引增量读取失败 mids=%d err=%v", len(mids), err)
		RequestActiveFilmSearchIndexRebuild()
		return false
	}
	if resealAfterRead != nil {
		resealAfterRead()
	}

	for {
		activeFilmSearchMetasMu.Lock()
		cur := activeFilmSearchMetas.Load()
		if !searchIndexCurrent(cur, version) {
			activeFilmSearchMetasMu.Unlock()
			log.Printf("[ActiveReadModel] 检索副本在封口前已失效，触发全量重建 mids=%d", len(mids))
			RequestActiveFilmSearchIndexRebuild()
			return false
		}
		if cur == base {
			sealAll := len(drop) > 0 || len(mids) >= searchResealSealMin
			var idx *filmSearchMetaIndex
			if sealAll {
				idx = sealSearchMetaIndex(cur.Version, patchSearchMetas(cur.Items, upsertMap, drop))
			} else {
				idx = updateSearchMetaIndexIncremental(cur, cur.Version, upsertMap)
			}
			activeFilmSearchMetas.Store(idx)
			activeFilmSearchMetasMu.Unlock()
			log.Printf("[ActiveReadModel] 检索索引按需封口 mids=%d drop=%d seal=%t total=%d",
				len(mids), len(drop), sealAll, len(idx.Items))
			return true
		}
		activeFilmSearchMetasMu.Unlock()
		base = cur
		upsertMap, drop, err = readSearchMetaUpserts(mids, nil)
		if err != nil {
			log.Printf("[ActiveReadModel] 检索索引封口重读失败 mids=%d err=%v", len(mids), err)
			RequestActiveFilmSearchIndexRebuild()
			return false
		}
	}
}

func searchIndexReady(cur *filmSearchMetaIndex, version string) bool {
	return cur != nil && cur.Version != "" && (version == "" || cur.Version == version)
}

func searchIndexCurrent(cur *filmSearchMetaIndex, version string) bool {
	return searchIndexReady(cur, version) && cur.Version == version
}

// resealAfterRead 仅测试使用：锁外读完后、加锁前替换索引，用来撞指针变化。
var resealAfterRead func()

// readSearchMetaUpserts 封口读库入口。测试可替换成失败。
var readSearchMetaUpserts = loadSearchMetaUpserts

func resealLoadProgress(total int64) func(n int) {
	next := resealProgressStep(total)
	var loaded int64
	return func(n int) {
		loaded += int64(n)
		if loaded < next && loaded < total {
			return
		}
		done := loaded
		if done > total {
			done = total
		}
		NotifySearchIndexProgress(total, done, "indexing")
		if loaded >= total {
			next = total + 1
			return
		}
		next = ((loaded / resealProgressStep(total)) + 1) * resealProgressStep(total)
	}
}

func resealProgressStep(total int64) int64 {
	step := total / 10
	if step < 1 {
		return total
	}
	return step
}

func loadSearchMetaUpserts(mids []int64, onBatch func(n int)) (map[int64]FilmSearchMeta, map[int64]struct{}, error) {
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
		if onBatch != nil {
			onBatch(end - i)
		}
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
