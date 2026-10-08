package snapshot

import (
	"fmt"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/repository/film/cache"
	"server/internal/repository/support"
)

const snapshotCacheDebounce = 5 * time.Second

var (
	cacheDebounceMu    sync.Mutex
	cacheDebounceTimer *time.Timer
	cacheDebounceMids  = make(map[int64]struct{})
	cacheDebounceVer   string
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
			config.DailyUpdatesV2CatCacheKey,
		)
	}
	cache.BumpSearchTagsVersion()
	cache.ClearPatterns(
		fmt.Sprintf("%s*", config.IndexPageCacheKey),
		fmt.Sprintf("%s*", config.IndexDailyUpdatesCacheKey),
		fmt.Sprintf("%s*", config.DailyUpdatesV2CatCacheKey),
		fmt.Sprintf("%s:*", config.TVBoxConfigCacheKey),
		fmt.Sprintf("%s:*", config.TVBoxList),
		fmt.Sprintf("%s:*", config.TVBoxNetworkConfigCacheKey),
		fmt.Sprintf("%s:*", config.FilmClassifyCacheKey),
		fmt.Sprintf("%s:*", config.FilmSearchTagsKey),
		fmt.Sprintf("%s:*", config.FilmFilterOptionKey),
		fmt.Sprintf("%s:*", config.DailyUpdatesV2CachePrefix),
	)
}

// filmListDerivedCachePatterns 快照派生的列表类缓存。增量发布不换快照版本，
// key 里的 v{version} 不会变，必须在写库后主动删掉，否则列表上的播放源摘要/备注会脏到 TTL。
func filmListDerivedCachePatterns() []string {
	return []string{
		config.FilmCategoryCachePrefix + ":*",
		config.FilmCategoryPageCachePrefix + ":*",
		config.FilmHotCachePrefix + ":*",
		config.FilmHotPoolCachePrefix + ":*",
		config.FilmSortCachePrefix + ":*",
		config.FilmHotKeywordsKey + ":*",
		config.FilmRelatePrefix + ":*",
	}
}

// ClearSearchCache 清除所有前台搜索缓存
func ClearSearchCache() {
	cache.ClearPatterns(config.FilmSearchCachePrefix + ":*")
}

func invalidateDeletedSnapshotCaches(version string, mids []int64) {
	invalidateSnapshotDataCaches(version, mids)
	RefreshAccessDataCaches()
}

func scheduleSnapshotCacheInvalidation(version string, mids []int64) {
	BumpSearchCacheVersion()
	deletePlayInfoCacheKeys(mids)
	cacheDebounceMu.Lock()
	defer cacheDebounceMu.Unlock()
	cacheDebounceVer = version
	for _, mid := range mids {
		if mid > 0 {
			cacheDebounceMids[mid] = struct{}{}
		}
	}
	if cacheDebounceTimer == nil {
		cacheDebounceTimer = time.AfterFunc(snapshotCacheDebounce, fireDebouncedSnapshotCacheInvalidation)
		return
	}
	cacheDebounceTimer.Reset(snapshotCacheDebounce)
}

func FlushSnapshotCacheInvalidation() {
	cacheDebounceMu.Lock()
	version := cacheDebounceVer
	mids := drainCacheDebounceMidsLocked()
	if cacheDebounceTimer != nil {
		cacheDebounceTimer.Stop()
		cacheDebounceTimer = nil
	}
	cacheDebounceVer = ""
	cacheDebounceMu.Unlock()
	if version == "" && len(mids) == 0 {
		RefreshAccessDataCaches()
		return
	}
	applyBroadSnapshotCacheInvalidation(version, mids)
}

func fireDebouncedSnapshotCacheInvalidation() {
	cacheDebounceMu.Lock()
	version := cacheDebounceVer
	mids := drainCacheDebounceMidsLocked()
	cacheDebounceTimer = nil
	cacheDebounceVer = ""
	cacheDebounceMu.Unlock()
	applyBroadSnapshotCacheInvalidation(version, mids)
}

func drainCacheDebounceMidsLocked() []int64 {
	if len(cacheDebounceMids) == 0 {
		return nil
	}
	mids := make([]int64, 0, len(cacheDebounceMids))
	for mid := range cacheDebounceMids {
		mids = append(mids, mid)
	}
	cacheDebounceMids = make(map[int64]struct{})
	return mids
}

func invalidateSnapshotDataCaches(version string, mids []int64) {
	applyBroadSnapshotCacheInvalidation(version, mids)
}

func applyBroadSnapshotCacheInvalidation(version string, mids []int64) {
	support.ClearIndexPageCache()
	if db.Rdb != nil {
		db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)
	}
	cache.ClearProvideListCache()
	BumpSearchCacheVersion()
	cache.ClearPatterns(filmListDerivedCachePatterns()...)
	deletePlayInfoCacheKeys(mids)
	RefreshAccessDataCaches()
}

func deletePlayInfoCacheKeys(mids []int64) {
	if db.Rdb == nil || len(mids) == 0 {
		return
	}
	const pipeBatchSize = 1000
	for i := 0; i < len(mids); i += pipeBatchSize {
		end := i + pipeBatchSize
		if end > len(mids) {
			end = len(mids)
		}
		pipe := db.Rdb.Pipeline()
		for _, mid := range mids[i:end] {
			pipe.Del(db.Cxt, fmt.Sprintf("%s:%d", config.FilmPlayInfoKey, mid))
		}
		_, _ = pipe.Exec(db.Cxt)
	}
	BumpPlayInfoGeneration()
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
	BumpPlayInfoGeneration()
}

// ClearDynamicPlayCaches 清除播放详情缓存与TVBox播放列表缓存
func ClearDynamicPlayCaches() {
	cache.ClearPatterns(
		config.FilmPlayInfoKey+":*",
		config.TVBoxList+":*",
	)
	BumpPlayInfoGeneration()
}

func ClearSnapshotState() {
	ClearActiveFilmReadModel()
	ClearActiveSnapshotVersion()
	if db.Rdb != nil {
		db.Rdb.Del(db.Cxt, config.SnapshotActiveVersionKey)
	}
	RefreshAccessDataCaches()
}
