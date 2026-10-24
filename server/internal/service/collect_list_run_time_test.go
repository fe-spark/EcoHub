package service

import (
	"testing"
	"time"

	"server/internal/model"
)

func TestGetFilmSourceList_UsesLastRunTime(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "src-run", Name: "站", Uri: "http://run", State: true}).Error; err != nil {
		t.Fatal(err)
	}
	anchor := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	runAt := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	if err := gdb.Create(&model.CollectSourceStats{
		SourceId:         "src-run",
		LastCollectTime:  &anchor,
		LastRunTime:      &runAt,
		LastRunPageCount: 666,
		SitePageTotal:    9999,
	}).Error; err != nil {
		t.Fatal(err)
	}

	list, err := (&CollectService{}).GetFilmSourceList()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d", len(list))
	}
	if list[0].LastRunTime == nil || !list[0].LastRunTime.Equal(runAt) {
		t.Fatalf("last run = %v, want %v", list[0].LastRunTime, runAt)
	}
	if list[0].LastRunPageCount != 666 {
		t.Fatalf("last run page count = %d, want 666", list[0].LastRunPageCount)
	}
	if list[0].SitePageTotal != 9999 {
		t.Fatalf("site page total = %d, want 9999", list[0].SitePageTotal)
	}
}
