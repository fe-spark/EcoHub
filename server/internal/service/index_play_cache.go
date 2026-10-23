package service

import (
	"time"

	"server/internal/infra/db"
	filmsnapshot "server/internal/repository/film/snapshot"
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
