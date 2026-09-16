package snapshot

import (
	"fmt"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/repository/film/cache"
	"server/internal/repository/support"
)

func RefreshAccessDataCaches() {
	if db.Rdb != nil {
		db.Rdb.Del(
			db.Cxt,
			config.ActiveCategoryTreeKey,
			config.CategoryTreeKey,
			config.TVBoxConfigCacheKey,
			config.BannersKey,
			config.IndexDailyUpdatesCacheKey,
		)
	}
	cache.BumpSearchTagsVersion()
	cache.ClearPatterns(
		fmt.Sprintf("%s*", config.IndexPageCacheKey),
		fmt.Sprintf("%s:*", config.TVBoxConfigCacheKey),
		fmt.Sprintf("%s:*", config.TVBoxList),
		fmt.Sprintf("%s:*", config.TVBoxNetworkConfigCacheKey),
		fmt.Sprintf("%s:*", config.FilmClassifyCacheKey),
		fmt.Sprintf("%s:*", config.FilmSearchTagsKey),
		fmt.Sprintf("%s:*", config.FilmFilterOptionKey),
	)
}

// ClearSearchCache 清除所有前台搜索缓存
func ClearSearchCache() {
	cache.ClearPatterns(config.FilmSearchCachePrefix + ":*")
}


func invalidateDeletedSnapshotCaches(version string, mids []int64) {
	invalidateSnapshotDataCaches(version, mids)
}

func invalidateSnapshotDataCaches(version string, mids []int64) {
	support.ClearIndexPageCache()
	if db.Rdb != nil {
		db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)
	}
	cache.ClearProvideListCache()
	BumpSearchCacheVersion()
	if db.Rdb != nil && len(mids) > 0 {
		// 精准批量删除被修改影片的详情与播放页缓存（按 1000 条分批下发，避免过大 Pipeline 占用缓冲区）
		const pipeBatchSize = 1000
		for i := 0; i < len(mids); i += pipeBatchSize {
			end := i + pipeBatchSize
			if end > len(mids) {
				end = len(mids)
			}
			pipe := db.Rdb.Pipeline()
			for _, mid := range mids[i:end] {
				pipe.Del(db.Cxt, fmt.Sprintf("%s:%d", config.FilmPlayInfoKey, mid))
				if version != "" {
					pipe.Del(db.Cxt, fmt.Sprintf("%s:v%s:%d", config.FilmRelateCandCachePrefix, version, mid))
					pipe.Del(db.Cxt, fmt.Sprintf("%s:v%s:%d:p1:s10", config.FilmRelateVOCachePrefix, version, mid))
					pipe.Del(db.Cxt, fmt.Sprintf("%s:v%s:%d:p1:s12", config.FilmRelateVOCachePrefix, version, mid))
					pipe.Del(db.Cxt, fmt.Sprintf("%s:v%s:%d:p1:s20", config.FilmRelateVOCachePrefix, version, mid))
				}
			}
			_, _ = pipe.Exec(db.Cxt)
		}
	}
}

func ClearAllSnapshotDynamicCaches() {
	support.ClearIndexPageCache()
	ClearActiveFilmReadModel()
	cache.ClearPatterns(
		config.FilmPlayInfoKey+":*",
		config.FilmHotKeywordsKey+":*",
		config.FilmCategoryCachePrefix+":*",
		config.FilmCategoryPageCachePrefix+":*",
		config.FilmHotCachePrefix+":*",
		config.FilmHotPoolCachePrefix+":*",
		config.FilmSortCachePrefix+":*",
		config.FilmSearchTagsKey+":*",
		config.ProvideListKey+":*",
		config.FilmSearchCachePrefix+":*",
		config.FilmRelatePrefix+":*",
		config.FilmClassifyCacheKey+":*",
		config.FilmFilterOptionKey+":*",
		config.TVBoxConfigCacheKey+":*",
		config.TVBoxList+":*",
		config.TVBoxNetworkConfigCacheKey+":*",
		config.IndexPageCacheKey+"*",
	)
}

// ClearDynamicPlayCaches 清除播放详情缓存与TVBox播放列表缓存
func ClearDynamicPlayCaches() {
	cache.ClearPatterns(
		config.FilmPlayInfoKey+":*",
		config.TVBoxList+":*",
	)
}

func ClearSnapshotState() {
	ClearActiveFilmReadModel()
	ClearActiveSnapshotVersion()
	if db.Rdb != nil {
		db.Rdb.Del(db.Cxt, config.SnapshotActiveVersionKey)
	}
	RefreshAccessDataCaches()
}
