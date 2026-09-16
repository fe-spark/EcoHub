package film

import (
	"fmt"
	"log"
	"path"
	"strings"
	"sync"

	"server/internal/config"
	"server/internal/infra/db"
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
	bumpSearchTagsCacheVersion()
	clearCachePatterns(
		fmt.Sprintf("%s*", config.IndexPageCacheKey),
		fmt.Sprintf("%s:*", config.TVBoxList),
		fmt.Sprintf("%s:*", config.TVBoxNetworkConfigCacheKey),
		fmt.Sprintf("%s:*", config.FilmClassifyCacheKey),
		fmt.Sprintf("%s:*", config.FilmSearchTagsKey),
		fmt.Sprintf("%s:*", config.FilmFilterOptionKey),
	)
}

var asyncClearSearchWg sync.WaitGroup

// ClearSearchCache 清除所有前台搜索缓存
func ClearSearchCache() {
	clearCachePatterns(config.FilmSearchCachePrefix + ":*")
}

func dispatchAsyncClearSearchCache() {
	asyncClearSearchWg.Add(1)
	go func() {
		defer asyncClearSearchWg.Done()
		ClearSearchCache()
	}()
}

// WaitAsyncClearSearchCacheDone 等待所有异步搜索缓存清理任务完成（供测试隔离与生命周期管理使用）
func WaitAsyncClearSearchCacheDone() {
	asyncClearSearchWg.Wait()
}

// InvalidateIncrementalSnapshotCaches 增量快照发布后精准淘汰列表/播放缓存，并重置内存搜索索引。
// 严禁调用 ClearActiveFilmReadModel：防止 Version 被置空导致全站播放详情失败。
func InvalidateIncrementalSnapshotCaches(version string, mids []int64) {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	// 增量从内存搜索索引中更新/新增 mids，避免粗暴清空导致下一次检索全库重算 19 秒
	if len(mids) > 0 {
		UpsertMidsToActiveFilmSearchIndex(version, mids)
	} else {
		InvalidateActiveFilmSearchIndex(version)
	}
	invalidateSnapshotDataCaches(version, mids)
}

func invalidateDeletedSnapshotCaches(version string, mids []int64) {
	invalidateSnapshotDataCaches(version, mids)
}

func invalidateSnapshotDataCaches(version string, mids []int64) {
	support.ClearIndexPageCache()
	if db.Rdb != nil {
		db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)
	}
	ClearProvideListCache()
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
			}
			_, _ = pipe.Exec(db.Cxt)
		}
	}
}

func ClearAllSnapshotDynamicCaches() {
	support.ClearIndexPageCache()
	ClearActiveFilmReadModel()
	clearCachePatterns(
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
		config.TVBoxList+":*",
		config.TVBoxNetworkConfigCacheKey+":*",
		config.IndexPageCacheKey+"*",
	)
}

// ClearDynamicPlayCaches 清除播放详情缓存与TVBox播放列表缓存
func ClearDynamicPlayCaches() {
	clearCachePatterns(
		config.FilmPlayInfoKey+":*",
		config.TVBoxList+":*",
	)
}

func ClearSnapshotState() {
	ClearActiveFilmReadModel()
	clearActiveSnapshotVersion()
	if db.Rdb != nil {
		db.Rdb.Del(db.Cxt, config.SnapshotActiveVersionKey)
	}
	RefreshAccessDataCaches()
}

func clearCachePatterns(patterns ...string) {
	if db.Rdb == nil || len(patterns) == 0 {
		return
	}

	validPatterns := make([]string, 0, len(patterns))
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p != "" {
			validPatterns = append(validPatterns, p)
		}
	}
	if len(validPatterns) == 0 {
		return
	}

	// 单一模式直接走单个 Redis SCAN
	if len(validPatterns) == 1 {
		scanAndDelPattern(validPatterns[0])
		return
	}

	// 只要模式数量 > 1，统一合并为单次 SCAN 遍历，客户端内存过滤，坚决避免串行发起 N 轮全库扫描
	cp := commonScanPrefix(validPatterns)
	scanPattern := "*"
	if cp != "" {
		scanPattern = cp + "*"
	} else if config.RedisKeyPrefix != "" {
		scanPattern = config.RedisKeyPrefix + ":*"
	}

	iter := db.Rdb.Scan(db.Cxt, 0, scanPattern, 1000).Iterator()
	var batch []string
	for iter.Next(db.Cxt) {
		key := iter.Val()
		if matchAnyPattern(key, validPatterns) {
			batch = append(batch, key)
			if len(batch) >= 100 {
				if err := db.Rdb.Del(db.Cxt, batch...).Err(); err != nil {
					log.Printf("clearCachePatterns Batch Del Error: count=%d err=%v", len(batch), err)
				}
				batch = batch[:0]
			}
		}
	}
	if len(batch) > 0 {
		if err := db.Rdb.Del(db.Cxt, batch...).Err(); err != nil {
			log.Printf("clearCachePatterns Final Batch Del Error: count=%d err=%v", len(batch), err)
		}
	}
	if err := iter.Err(); err != nil {
		log.Printf("clearCachePatterns Scan Error: pattern=%s err=%v", scanPattern, err)
	}
}

func scanAndDelPattern(pattern string) {
	iter := db.Rdb.Scan(db.Cxt, 0, pattern, config.MaxScanCount).Iterator()
	var batch []string
	for iter.Next(db.Cxt) {
		batch = append(batch, iter.Val())
		if len(batch) >= 100 {
			if err := db.Rdb.Del(db.Cxt, batch...).Err(); err != nil {
				log.Printf("scanAndDelPattern Batch Del Error: count=%d err=%v", len(batch), err)
			}
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := db.Rdb.Del(db.Cxt, batch...).Err(); err != nil {
			log.Printf("scanAndDelPattern Final Batch Del Error: count=%d err=%v", len(batch), err)
		}
	}
	if err := iter.Err(); err != nil {
		log.Printf("scanAndDelPattern Scan Error: pattern=%s err=%v", pattern, err)
	}
}

func commonScanPrefix(patterns []string) string {
	if len(patterns) == 0 {
		return ""
	}
	prefix := cleanPrefixBeforeGlob(patterns[0])
	for _, p := range patterns[1:] {
		clean := cleanPrefixBeforeGlob(p)
		for !strings.HasPrefix(clean, prefix) {
			if len(prefix) == 0 {
				return ""
			}
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

func cleanPrefixBeforeGlob(p string) string {
	if idx := strings.IndexAny(p, "*?["); idx != -1 {
		return p[:idx]
	}
	return p
}

func matchAnyPattern(key string, patterns []string) bool {
	for _, p := range patterns {
		if matchPattern(p, key) {
			return true
		}
	}
	return false
}

func matchPattern(pattern, key string) bool {
	if pattern == key {
		return true
	}
	if strings.HasSuffix(pattern, "*") && strings.Count(pattern, "*") == 1 && !strings.ContainsAny(pattern, "?[") {
		return strings.HasPrefix(key, pattern[:len(pattern)-1])
	}
	matched, _ := path.Match(pattern, key)
	if !matched && strings.Contains(key, "/") {
		escapedPattern := strings.ReplaceAll(pattern, "/", "\x00")
		escapedKey := strings.ReplaceAll(key, "/", "\x00")
		m, _ := path.Match(escapedPattern, escapedKey)
		return m
	}
	return matched
}
