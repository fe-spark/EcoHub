package cache

import (
	"fmt"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
)

// ClearSearchTagsCache 清除所有分类的复合搜索标签缓存。
func ClearSearchTagsCache(pid int64) {
	if db.Rdb == nil {
		return
	}
	pattern := fmt.Sprintf("%s:*", config.SearchTags)
	ctx := db.Cxt
	iter := db.Rdb.Scan(ctx, 0, pattern, config.MaxScanCount).Iterator()
	for iter.Next(ctx) {
		db.Rdb.Del(ctx, iter.Val())
	}
	BumpSearchTagsVersion()
}

// ClearAllSearchTagsCache 清除所有分类的搜索标签缓存 (扫描清理)
func ClearAllSearchTagsCache() {
	if db.Rdb == nil {
		return
	}
	pattern := config.SearchTags + ":*"
	iter := db.Rdb.Scan(db.Cxt, 0, pattern, config.MaxScanCount).Iterator()
	for iter.Next(db.Cxt) {
		db.Rdb.Del(db.Cxt, iter.Val())
	}
	BumpSearchTagsVersion()
	ClearTVBoxConfigCache()
}

// BumpSearchTagsVersion 递增搜索标签缓存版本号（Redis 不可用时静默跳过）。
func BumpSearchTagsVersion() {
	if db.Rdb == nil {
		return
	}
	db.Rdb.Set(db.Cxt, config.SearchTagsVersionKey, time.Now().UnixNano(), 0)
}

// GetSearchTagsVersion 读取搜索标签缓存版本号，缺失时回填当前时间戳。
func GetSearchTagsVersion() string {
	if db.Rdb == nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	version, err := db.Rdb.Get(db.Cxt, config.SearchTagsVersionKey).Result()
	if err == nil && version != "" {
		return version
	}
	version = fmt.Sprintf("%d", time.Now().UnixNano())
	db.Rdb.Set(db.Cxt, config.SearchTagsVersionKey, version, 0)
	return version
}
