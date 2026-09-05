package film

import (
	"testing"

	"server/internal/config"
	"server/internal/infra/db"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestClearFilmIndexCachesKeepsFrontKeys(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	orig := db.Rdb
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	db.Rdb = client
	t.Cleanup(func() {
		_ = client.Close()
		db.Rdb = orig
	})

	treeKey := config.ActiveCategoryTreeKey
	indexKey := config.IndexPageCacheKey + ":vkeep"
	dailyKey := config.IndexDailyUpdatesCacheKey
	if err := mr.Set(treeKey, `{"id":0}`); err != nil {
		t.Fatalf("seed tree: %v", err)
	}
	if err := mr.Set(indexKey, `{"ok":1}`); err != nil {
		t.Fatalf("seed index: %v", err)
	}
	if err := mr.Set(dailyKey, `[]`); err != nil {
		t.Fatalf("seed daily: %v", err)
	}

	clearDetailCaches(1)
	clearDetailCaches(0)
	clearFilmIndexCachesByPidSet(map[int64]struct{}{
		1:  {},
		0:  {},
		-1: {},
	})

	if got, err := mr.Get(treeKey); err != nil || got != `{"id":0}` {
		t.Fatalf("ActiveCategoryTreeKey dropped: got %q err %v", got, err)
	}
	if got, err := mr.Get(indexKey); err != nil || got != `{"ok":1}` {
		t.Fatalf("IndexPageCache dropped: got %q err %v", got, err)
	}
	if got, err := mr.Get(dailyKey); err != nil || got != `[]` {
		t.Fatalf("IndexDailyUpdatesCache dropped: got %q err %v", got, err)
	}
}
