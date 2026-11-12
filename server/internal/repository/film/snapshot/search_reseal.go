package snapshot

import (
	"context"
	"log"
	"time"

	"golang.org/x/sync/errgroup"

	"server/internal/infra/db"
	"server/internal/model"
)

// ResealSearchIndexMids 按编号封口。
func ResealSearchIndexMids(mids []int64) bool {
	return ResealSearchIndex(mids)
}

// ResealSearchIndex 全部采集队列结束后的唯一封口。
// 内存索引不可用或还是空的，整表顺序重建。
// 点查失败同样改为顺序加载。成功打进内存索引时返回 true。
func ResealSearchIndex(mids []int64) bool {
	startedAt := time.Now()
	mids = NormalizeSnapshotMIDs(mids)
	log.Printf("[ActiveReadModel] 检索索引封口进入 raw=%d", len(mids))
	if len(mids) == 0 || db.Mdb == nil {
		return false
	}

	version := currentSearchIndexVersion()
	base := activeFilmSearchMetas.Load()
	items := 0
	if base != nil {
		items = len(base.Items)
	}
	if !searchIndexReady(base, version) || items == 0 {
		log.Printf("[ActiveReadModel] 检索索引整表顺序加载 mids=%d items=%d", len(mids), items)
		RequestActiveFilmSearchIndexRebuild()
		return false
	}

	total := int64(len(mids))
	baseCount := 0
	if base != nil {
		baseCount = len(base.Items)
	}
	log.Printf("[ActiveReadModel] 检索索引封口启动 mids=%d 当前内存条目=%d version=%s", total, baseCount, version)
	NotifySearchIndexProgress(total, 0, "indexing")
	upsertMap, drop, err := readSearchMetaUpserts(mids, resealLoadProgress(total, startedAt))
	if err != nil {
		log.Printf("[ActiveReadModel] 检索索引增量读取失败 mids=%d err=%v 耗时=%s", len(mids), err, time.Since(startedAt).Round(time.Millisecond))
		RequestActiveFilmSearchIndexRebuild()
		return false
	}
	dbCost := time.Since(startedAt)
	log.Printf("[ActiveReadModel] 检索索引数据读取完成 upsert=%d drop=%d 耗时=%s，开始构建内存索引...",
		len(upsertMap), len(drop), dbCost.Round(time.Millisecond))
	if resealAfterRead != nil {
		resealAfterRead()
	}

	buildStart := time.Now()
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
			var idx *filmSearchMetaIndex
			hasDrop := len(drop) > 0
			log.Printf("[ActiveReadModel] 检索索引内存构建启动 mids=%d drop=%d seal=%t", len(mids), len(drop), hasDrop)
			if hasDrop {
				idx = sealSearchMetaIndex(cur.Version, patchSearchMetas(cur.Items, upsertMap, drop))
			} else {
				idx = updateSearchMetaIndexIncremental(cur, cur.Version, upsertMap)
			}
			activeFilmSearchMetas.Store(idx)
			activeFilmSearchMetasMu.Unlock()
			buildCost := time.Since(buildStart)
			log.Printf("[ActiveReadModel] 检索索引按需封口完成 mids=%d drop=%d seal=%t total=%d 耗时=%s (读库=%s 构建=%s)",
				len(mids), len(drop), hasDrop, len(idx.Items),
				time.Since(startedAt).Round(time.Millisecond), dbCost.Round(time.Millisecond), buildCost.Round(time.Millisecond))
			NotifySearchIndexProgress(total, total, "done")
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

func resealLoadProgress(total int64, startedAt time.Time) func(n int) {
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
		pct := float64(done) * 100 / float64(total)
		log.Printf("[ActiveReadModel] 检索索引读库进度: %d/%d (%.1f%%) 耗时=%s",
			done, total, pct, time.Since(startedAt).Round(time.Millisecond))
		// 读完不等于封完。满格只由构建结束后的 done 发。
		if done < total {
			NotifySearchIndexProgress(total, done, "indexing")
		}
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
	if len(mids) == 0 {
		return make(map[int64]FilmSearchMeta), make(map[int64]struct{}), nil
	}
	drop := make(map[int64]struct{}, len(mids))
	for _, id := range mids {
		drop[id] = struct{}{}
	}
	const batchSize = 2500
	allRows := make([]dbMetaRow, 0, len(mids))
	var plays map[int64][]string
	g, _ := errgroup.WithContext(context.Background())
	g.Go(func() error {
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
				return err
			}
			allRows = append(allRows, batchRows...)
			if onBatch != nil {
				onBatch(end - i)
			}
		}
		return nil
	})
	g.Go(func() error {
		var err error
		plays, err = loadPlaySources(mids)
		return err
	})
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}
	log.Printf("[ActiveReadModel] 检索索引元数据与播放线路读取完成 rows=%d mids=%d", len(allRows), len(mids))
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
func patchSearchMetas(items []*FilmSearchMeta, upsert map[int64]FilmSearchMeta, drop map[int64]struct{}) []FilmSearchMeta {
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
		out = append(out, *it)
	}
	for mid, it := range upsert {
		if _, ok := seen[mid]; !ok {
			out = append(out, it)
		}
	}
	return out
}
