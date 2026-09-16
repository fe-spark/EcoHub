package service

import (
	"testing"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	filmsnapshot "server/internal/repository/film/snapshot"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestStoreFilmPlayInfoCache_DropsStaleWrite(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	orig := db.Rdb
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	db.Rdb = client
	t.Cleanup(func() {
		_ = client.Close()
		db.Rdb = orig
	})

	key := config.FilmPlayInfoKey + ":47014"
	gen := filmsnapshot.PlayInfoGeneration()
	filmsnapshot.BumpPlayInfoGeneration()
	storeFilmPlayInfoCache(key, `{"stale":true}`, time.Hour, gen)
	if client.Exists(db.Cxt, key).Val() != 0 {
		t.Fatal("stale play info must not be written after generation bump")
	}

	fresh := filmsnapshot.PlayInfoGeneration()
	storeFilmPlayInfoCache(key, `{"ok":true}`, time.Hour, fresh)
	if client.Get(db.Cxt, key).Val() != `{"ok":true}` {
		t.Fatal("current generation should still be able to write")
	}
}
