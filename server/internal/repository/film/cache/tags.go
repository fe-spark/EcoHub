package cache

import (
	"fmt"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
)

// ClearSearchTagsCache 清除搜索标签与筛选项缓存。
func ClearSearchTagsCache(_ int64) {
	ClearAllSearchTagsCache()
}

// ClearAllSearchTagsCache 清除所有分类的搜索标签及筛选项缓存（单次流式扫描 + 批量 UNLINK）。
func ClearAllSearchTagsCache() {
	if db.Rdb == nil {
		return
	}
	ClearPatterns(
		config.SearchTags+":*",
		config.FilmFilterOptionKey+":*",
	)
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
