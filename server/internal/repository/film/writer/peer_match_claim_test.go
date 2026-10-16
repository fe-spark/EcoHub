package writer

import (
	"context"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/film/shared"

	"gorm.io/gorm"
)

func TestBestMidPrefersDoubanOverSmallerTitle(t *testing.T) {
	rows := []matchKeyMid{{MatchKey: "title", Mid: 1}, {MatchKey: "db", Mid: 9}}
	ranks := map[string]int{"db": 0, "title": 2}
	if got := bestMidForKeys([]string{"db", "title"}, ranks, rows); got != 9 {
		t.Fatalf("got %d", got)
	}
}

func TestClaimDoesNotCopyStrongKeysOntoTitleMid(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	titleKeys := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 0)
	strong := shared.BuildMovieMatchKeysWithCategory(36117912, "仙逆", 3)
	if len(titleKeys) != 1 || len(strong) < 2 {
		t.Fatalf("title=%v strong=%v", titleKeys, strong)
	}
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1},
		FilmIndexContent:  model.FilmIndexContent{Name: "仙逆"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.MovieMatchKey{Mid: 1, MatchKey: titleKeys[0]}).Error; err != nil {
		t.Fatal(err)
	}

	detail := testPeerDetail(81, "仙逆", "第1集")
	detail.DbId = 36117912
	detail.Pid = 3
	detail.RawPid = 3
	var mid int64
	err := gdb.Transaction(func(tx *gorm.DB) error {
		claimed, film, err := claimPeerFilmByMatchKeysTx(tx, &model.FilmSource{Id: "src-claim", Name: "甲", State: true}, detail)
		if err != nil {
			return err
		}
		if film == nil {
			t.Fatal("expected a claimed film")
		}
		mid = claimed
		return nil
	})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if mid != 1 {
		t.Fatalf("mid=%d, want title match 1", mid)
	}
	var stored []model.MovieMatchKey
	if err := gdb.Where("mid = ?", 1).Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	got := make(map[string]struct{}, len(stored))
	for _, key := range stored {
		got[key.MatchKey] = struct{}{}
	}
	for _, key := range strong {
		if key == titleKeys[0] {
			continue
		}
		if _, ok := got[key]; ok {
			t.Fatalf("strong key copied onto title mid: %s", key)
		}
	}
}

func TestBatchClaimPrefersDoubanMid(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	titleKeys := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 0)
	strong := shared.BuildMovieMatchKeysWithCategory(36117912, "仙逆", 0)
	if len(titleKeys) != 1 || len(strong) < 1 {
		t.Fatalf("title=%v strong=%v", titleKeys, strong)
	}
	for _, row := range []model.FilmIndex{
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1}, FilmIndexContent: model.FilmIndexContent{Name: "仅片名"}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2}, FilmIndexContent: model.FilmIndexContent{Name: "豆瓣片"}},
	} {
		if err := gdb.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := gdb.Create(&model.MovieMatchKey{Mid: 1, MatchKey: titleKeys[0]}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.MovieMatchKey{Mid: 2, MatchKey: strong[0]}).Error; err != nil {
		t.Fatal(err)
	}

	detail := testPeerDetail(82, "仙逆", "第1集")
	detail.DbId = 36117912
	if _, err := SaveCollectedPeerDetails(context.Background(), &model.FilmSource{Id: "src-batch", Name: "乙", State: true}, 1, []model.MovieDetail{detail}); err != nil {
		t.Fatal(err)
	}
	var mapping model.MovieSourceMapping
	if err := gdb.Where("source_id = ? AND source_mid = ?", "src-batch", 82).First(&mapping).Error; err != nil {
		t.Fatal(err)
	}
	if mapping.GlobalMid != 2 {
		t.Fatalf("claimed mid=%d, want douban mid 2", mapping.GlobalMid)
	}
	var copied int64
	gdb.Model(&model.MovieMatchKey{}).Where("mid = ? AND match_key = ?", 1, strong[0]).Count(&copied)
	if copied != 0 {
		t.Fatal("douban key was copied onto the smaller title mid")
	}
}
