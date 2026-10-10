package snapshot

import (
	"time"

	"server/internal/infra/db"
)

const emptyListCacheTTL = 60 * time.Second

// writeListCache 在检索世代未变时写入列表缓存。失效发生在查询期间时放弃回写，避免旧结果盖住已删除的键。
func writeListCache(key string, payload []byte, ttl time.Duration, gen string) {
	if db.Rdb == nil || key == "" || len(payload) == 0 {
		return
	}
	if gen != "" && GetSearchCacheVersion() != gen {
		return
	}
	_ = db.Rdb.Set(db.Cxt, key, string(payload), ttl).Err()
}

func listCacheTTL(count int, full time.Duration) time.Duration {
	if count <= 0 {
		return emptyListCacheTTL
	}
	return full
}
