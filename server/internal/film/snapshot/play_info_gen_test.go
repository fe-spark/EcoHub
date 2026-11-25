package snapshot

import (
	"testing"

	"server/internal/config"
	"server/internal/infra/db"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestPlayInfoGeneration_BumpChangesValue(t *testing.T) {
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

	before := PlayInfoGeneration()
	BumpPlayInfoGeneration()
	after := PlayInfoGeneration()
	if after <= before {
		t.Fatalf("expected generation to increase, before=%d after=%d", before, after)
	}
	if mr.Exists(config.FilmPlayInfoGenKey) == false {
		t.Fatalf("expected %s to exist", config.FilmPlayInfoGenKey)
	}
}
