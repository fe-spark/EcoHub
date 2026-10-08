package film

import (
	"fmt"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupResetStatsDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(
		&model.FilmSource{},
		&model.FilmIndex{},
		&model.FilmSourcePlaylist{},
		&model.FilmListSnapshot{},
		&model.FilmSnapshotSource{},
		&model.Category{},
		&model.FailureRecord{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Mdb = gdb
	db.Rdb = nil
	return gdb
}

func TestGetResetImpactStats_BaselineSourceOnly(t *testing.T) {
	gdb := setupResetStatsDB(t)

	gdb.Create(&model.FilmSource{Id: "src_a", Name: "首选站A", Uri: "http://a.test", State: true, Sort: 0})
	gdb.Create(&model.FilmSource{Id: "src_b", Name: "备用站B", Uri: "http://b.test", State: true, Sort: 1})

	gdb.Create(&model.FilmIndex{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1}})
	gdb.Create(&model.FilmIndex{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2}})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 1, SourceId: "src_a", LineKind: "play", GroupIndex: 0, Content: "[]"})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 2, SourceId: "src_b", LineKind: "play", GroupIndex: 0, Content: "[]"})

	gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电影", Show: true, Sort: 1})
	gdb.Create(&model.FailureRecord{OriginId: "src_a", OriginName: "首选站A", Cause: "a"})
	gdb.Create(&model.FailureRecord{OriginId: "src_b", OriginName: "备用站B", Cause: "b"})
	gdb.Create(&model.FailureRecord{OriginId: "src_b", OriginName: "备用站B", Cause: "b2"})

	stats := GetResetImpactStats()
	if stats.SourceId != "src_a" || stats.SourceName != "首选站A" {
		t.Fatalf("expected primary src_a, got id=%s name=%s", stats.SourceId, stats.SourceName)
	}
	if stats.Films != 1 {
		t.Fatalf("expected 1 baseline film, got %d", stats.Films)
	}
	if stats.Failures != 1 {
		t.Fatalf("expected 1 baseline failure, got %d", stats.Failures)
	}
}
