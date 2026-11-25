package cache

import (
	"server/internal/config"
	"server/internal/infra/db"
)

// ClearProvideListCache 清除 TVBox 播放列表与提供列表缓存。
func ClearProvideListCache() {
	ClearPatterns(config.TVBoxList+":*", config.ProvideListKey+":*")
}

// ClearTVBoxConfigCache 清除 TVBox 配置缓存。
func ClearTVBoxConfigCache() {
	if db.Rdb == nil {
		return
	}
	db.Rdb.Del(db.Cxt, config.TVBoxConfigCacheKey)
	scanAndDelPattern(config.TVBoxConfigCacheKey + ":*")
}

// ClearTVBoxListCache 清除 TVBox 列表缓存。
func ClearTVBoxListCache() {
	if db.Rdb == nil {
		return
	}
	scanAndDelPattern(config.TVBoxList + ":*")
}
