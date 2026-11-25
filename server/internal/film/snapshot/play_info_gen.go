package snapshot

import (
	"errors"
	"sync/atomic"

	"server/internal/config"
	"server/internal/infra/db"

	"github.com/redis/go-redis/v9"
)

var playInfoGen atomic.Int64

// BumpPlayInfoGeneration 播放详情缓存失效时调用。世代变了，进行中的 GetFilmDetail 不得把旧结果写回 Redis。
func BumpPlayInfoGeneration() {
	if db.Rdb != nil {
		n, err := db.Rdb.Incr(db.Cxt, config.FilmPlayInfoGenKey).Result()
		if err == nil {
			playInfoGen.Store(n)
			return
		}
	}
	playInfoGen.Add(1)
}

// PlayInfoGeneration 当前播放详情缓存世代。Redis 是权威；key 不存在视为 0。
func PlayInfoGeneration() int64 {
	if db.Rdb != nil {
		n, err := db.Rdb.Get(db.Cxt, config.FilmPlayInfoGenKey).Int64()
		if err == nil {
			playInfoGen.Store(n)
			return n
		}
		if errors.Is(err, redis.Nil) {
			playInfoGen.Store(0)
			return 0
		}
	}
	return playInfoGen.Load()
}
