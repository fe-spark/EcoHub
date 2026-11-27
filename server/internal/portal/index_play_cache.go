package portal

import (
	"encoding/json"
	"math/rand"
	"time"

	filmsnapshot "server/internal/film/snapshot"
	"server/internal/infra/db"
	"server/internal/model"
)

func storeFilmPlayInfoCache(cacheKey, payload string, ttl time.Duration, gen int64) {
	if db.Rdb == nil {
		return
	}
	if filmsnapshot.PlayInfoGeneration() != gen {
		return
	}
	_ = db.Rdb.Set(db.Cxt, cacheKey, payload, ttl).Err()
	if filmsnapshot.PlayInfoGeneration() != gen {
		_ = db.Rdb.Del(db.Cxt, cacheKey).Err()
	}
}

func storeFilmPlayInfoBatchCache(items map[string]model.MovieDetailVo, baseTTL time.Duration, gen int64) {
	if db.Rdb == nil || len(items) == 0 || filmsnapshot.PlayInfoGeneration() != gen {
		return
	}
	pipe := db.Rdb.Pipeline()
	keys := make([]string, 0, len(items))
	for cacheKey, vo := range items {
		b, err := json.Marshal(vo)
		if err != nil {
			continue
		}
		jitter := time.Duration(rand.Intn(900)) * time.Second
		pipe.Set(db.Cxt, cacheKey, string(b), baseTTL+jitter)
		keys = append(keys, cacheKey)
	}
	if len(keys) == 0 {
		return
	}
	_, _ = pipe.Exec(db.Cxt)
	if filmsnapshot.PlayInfoGeneration() != gen {
		_ = db.Rdb.Del(db.Cxt, keys...).Err()
	}
}
