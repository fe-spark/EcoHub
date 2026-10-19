package snapshot

import (
	"time"

	"server/internal/infra/db"
)

const emptyListCacheTTL = 60 * time.Second

// writeListCache 在检索世代未变时写入列表缓存。失效发生在查询期间时放弃回写，避免旧结果盖住已删除的键。
func writeListCache(key string, payload []byte, ttl time.Duration, gen string) {
	if len(payload) == 0 {
		return
	}
	StoreCacheIfCurrent(key, string(payload), ttl, gen)
}

// StoreCacheIfCurrent 在检索世代未变时写入。写入期间世代变了就删掉，避免旧页面盖住已失效的缓存。
func StoreCacheIfCurrent(key, payload string, ttl time.Duration, gen string) {
	if db.Rdb == nil || key == "" || payload == "" {
		return
	}
	if gen != "" && GetSearchCacheVersion() != gen {
		return
	}
	_ = db.Rdb.Set(db.Cxt, key, payload, ttl).Err()
	if gen != "" && GetSearchCacheVersion() != gen {
		_ = db.Rdb.Del(db.Cxt, key).Err()
	}
}

func listCacheTTL(count int, full time.Duration) time.Duration {
	if count <= 0 {
		return emptyListCacheTTL
	}
	return full
}
