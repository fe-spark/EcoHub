package snapshot

import (
	"fmt"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/film/cache"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repobase"
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
		)
	}
	BumpSearchCacheVersion()
	repobase.BumpIndexPageGeneration()
	cache.BumpSearchTagsVersion()
	cache.ClearPatterns(
		fmt.Sprintf("%s*", config.IndexPageCacheKey),
		fmt.Sprintf("%s:*", config.TVBoxConfigCacheKey),
		fmt.Sprintf("%s:*", config.TVBoxList),
		fmt.Sprintf("%s:*", config.TVBoxNetworkConfigCacheKey),
		fmt.Sprintf("%s:*", config.FilmClassifyCacheKey),
		fmt.Sprintf("%s:*", config.FilmCategoryCachePrefix),
		fmt.Sprintf("%s:*", config.FilmCategoryPageCachePrefix),
		fmt.Sprintf("%s:*", config.ProvideListKey),
		fmt.Sprintf("%s:*", config.FilmRelatePrefix),
		fmt.Sprintf("%s:*", config.FilmHotKeywordsKey),
		fmt.Sprintf("%s:*", config.FilmSearchCachePrefix),
		fmt.Sprintf("%s:*", config.FilmSearchTagsKey),
		fmt.Sprintf("%s:*", config.FilmFilterOptionKey),
	)
}

// filmListDerivedCachePatterns 快照派生的列表、分类首页和首页缓存。增量发布不换快照版本，
// key 里的版本不会变，必须在写库后主动删掉，否则影片和热播会脏到 TTL。
func filmListDerivedCachePatterns() []string {
	return []string{
		config.FilmCategoryCachePrefix + ":*",
		config.FilmCategoryPageCachePrefix + ":*",
		config.FilmHotCachePrefix + ":*",
		config.FilmHotPoolCachePrefix + ":*",
		config.FilmSortCachePrefix + ":*",
		config.FilmHotKeywordsKey + ":*",
		config.FilmRelatePrefix + ":*",
		config.FilmClassifyCacheKey + ":*",
		config.FilmSearchTagsKey + ":*",
		config.FilmFilterOptionKey + ":*",
		config.ProvideListKey + ":*",
		config.IndexPageCacheKey + "*",
		config.TVBoxConfigCacheKey + ":*",
	}
}

// ClearDerivedFilmListCaches 清掉带影片内容的列表、分类首页和首页缓存。
func ClearDerivedFilmListCaches() {
	cache.ClearPatterns(filmListDerivedCachePatterns()...)
}

// ClearSearchCache 清除所有前台搜索缓存
func ClearSearchCache() {
	cache.ClearPatterns(config.FilmSearchCachePrefix + ":*")
}

func invalidateDeletedSnapshotCaches(version string, mids []int64) {
	invalidateSnapshotDataCaches(version, mids)
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
	mids := drainCacheDebounceMidsLocked()
	cacheDebounceTimer = nil
	cacheDebounceVer = ""
	cacheDebounceMu.Unlock()
	cache.ClearProvideListCache()
	BumpSearchCacheVersion()
	cache.ClearPatterns(filmListDerivedCachePatterns()...)
	deletePlayInfoCacheKeys(mids)
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
	repobase.ClearIndexPageCache()
	if db.Rdb != nil {
		if err := db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey).Err(); err != nil {
			syslog.Errorf("[Snapshot] 清理分类树缓存失败: %v", err)
		}
	}
	cache.ClearProvideListCache()
	BumpSearchCacheVersion()
	cache.ClearPatterns(filmListDerivedCachePatterns()...)
	if err := deletePlayInfoCacheKeys(mids); err != nil {
		syslog.Errorf("[Snapshot] 清理播放详情缓存失败: %v", err)
	}
	RefreshAccessDataCaches()
}

// ExpandPlayInfoPeerMids 查询并包含与输入 mid 共享 match_key 的同片组关联 mid。
func ExpandPlayInfoPeerMids(mids []int64) []int64 {
	return expandPlayInfoPeerMids(mids)
}

// DeletePlayInfoCacheKeys 失效传入影片及同 match_key 关联片（如主站）的播放详情缓存。
func DeletePlayInfoCacheKeys(mids []int64) error {
	return deletePlayInfoCacheKeys(mids)
}

func expandPlayInfoPeerMids(mids []int64) []int64 {
	if len(mids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(mids))
	out := make([]int64, 0, len(mids))
	for _, mid := range mids {
		if mid > 0 {
			if _, ok := seen[mid]; !ok {
				seen[mid] = struct{}{}
				out = append(out, mid)
			}
		}
	}
	if db.Mdb == nil || len(out) == 0 {
		return out
	}
	const chunkSize = 500
	for i := 0; i < len(mids); i += chunkSize {
		end := min(i+chunkSize, len(mids))
		var peers []int64
		if err := db.Mdb.Table(model.TableMovieMatchKey+" AS m1").
			Select("DISTINCT m2.mid").
			Joins("JOIN "+model.TableMovieMatchKey+" AS m2 ON m1.match_key = m2.match_key").
			Where("m1.mid IN ?", mids[i:end]).
			Pluck("m2.mid", &peers).Error; err == nil {
			for _, p := range peers {
				if p > 0 {
					if _, ok := seen[p]; !ok {
						seen[p] = struct{}{}
						out = append(out, p)
					}
				}
			}
		}
	}
	return out
}

func deletePlayInfoCacheKeys(mids []int64) error {
	if db.Rdb == nil || len(mids) == 0 {
		return nil
	}
	BumpPlayInfoGeneration()
	if len(mids) > 2000 {
		cache.ClearPatterns(config.FilmPlayInfoKey + ":*")
		return nil
	}
	allMids := expandPlayInfoPeerMids(mids)
	const pipeBatchSize = 500
	for i := 0; i < len(allMids); i += pipeBatchSize {
		end := min(i+pipeBatchSize, len(allMids))
		pipe := db.Rdb.Pipeline()
		for _, mid := range allMids[i:end] {
			pipe.Unlink(db.Cxt,
				fmt.Sprintf("%s:%d", config.FilmPlayInfoKey, mid),
				fmt.Sprintf("%s:vod:%d", config.FilmPlayInfoKey, mid),
			)
		}
		if _, err := pipe.Exec(db.Cxt); err != nil {
			return err
		}
	}
	return nil
}

func ClearAllSnapshotDynamicCaches() {
	BumpPlayInfoGeneration()
	repobase.ClearIndexPageCache()
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
	BumpPlayInfoGeneration()
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
