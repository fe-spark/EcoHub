package service

import (
	"testing"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
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

func TestOrganizePlaySources_KeepsSourcesIndependent(t *testing.T) {
	lines := []model.PlayLinkVo{
		{
			Id:       "srcA#0",
			SourceId: "srcA",
			Name:     "金鹰源",
			LinkList: []model.MovieUrlInfo{
				{Episode: "第01集", Link: "http://a.com/1.m3u8"},
			},
		},
		{
			Id:       "srcB#0",
			SourceId: "srcB",
			Name:     "速博源",
			LinkList: []model.MovieUrlInfo{
				{Episode: "第1集", Link: "http://b.com/1.m3u8"},
				{Episode: "第2集", Link: "http://b.com/2.m3u8"},
			},
		},
	}

	res := OrganizePlaySources(lines, "srcA")
	if len(res[0].LinkList) != 1 {
		t.Fatalf("expected exactly 1 episode in first line (no cross-source fallback), got %d", len(res[0].LinkList))
	}
	if len(res[1].LinkList) != 2 {
		t.Fatalf("expected exactly 2 episodes in second line, got %d", len(res[1].LinkList))
	}
}

