package repobase

import (
	"sync/atomic"

	"server/internal/config"
	"server/internal/infra/db"
)

var indexPageGen atomic.Uint64

// IndexPageGeneration 返回首页缓存世代，防止并发读在缓存失效后把旧首页结果写回 Redis。
func IndexPageGeneration() uint64 {
	return indexPageGen.Load()
}

// BumpIndexPageGeneration 递增首页缓存世代。
func BumpIndexPageGeneration() {
	indexPageGen.Add(1)
}

// ClearIndexPageCache 清理首页相关缓存并递增世代，防止在途读回填旧数据。
func ClearIndexPageCache() {
	BumpIndexPageGeneration()
	unlinkKeysByPattern(config.IndexPageCacheKey + "*")
}

// ClearProvideNetworkConfigCache 清理对外聚合网关网络配置缓存。
func ClearProvideNetworkConfigCache() {
	unlinkKeysByPattern(config.TVBoxNetworkConfigCacheKey + ":*")
}

func unlinkKeysByPattern(pattern string) {
	rdb := db.Rdb
	if rdb == nil {
		return
	}
	const batchSize = 500
	batch := make([]string, 0, batchSize)
	iter := rdb.Scan(db.Cxt, 0, pattern, config.MaxScanCount).Iterator()
	for iter.Next(db.Cxt) {
		batch = append(batch, iter.Val())
		if len(batch) >= batchSize {
			_ = rdb.Unlink(db.Cxt, batch...).Err()
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		_ = rdb.Unlink(db.Cxt, batch...).Err()
	}
}
