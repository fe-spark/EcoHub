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
		&model.Category{},
		&model.CategoryMapping{},
		&model.FailureRecord{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Mdb = gdb
	db.Rdb = nil
	return gdb
}

func TestGetInventoryStats_LibraryAndSources(t *testing.T) {
	gdb := setupResetStatsDB(t)

	gdb.Create(&model.FilmSource{Id: "src_a", Name: "首选站A", Uri: "http://a.test", State: true, Sort: 0})
	gdb.Create(&model.FilmSource{Id: "src_b", Name: "备用站B", Uri: "http://b.test", State: true, Sort: 1})
	gdb.Create(&model.FilmSource{Id: "src_off", Name: "停用站", Uri: "http://off.test", State: false, Sort: 2})

	for _, mid := range []int64{1, 2, 3, 4, 5} {
		gdb.Create(&model.FilmIndex{FilmIndexIdentity: model.FilmIndexIdentity{Mid: mid}})
	}
	gdb.Delete(&model.FilmIndex{}, "mid = ?", int64(5))

	play := func(mid int64, sourceID string, group int) {
		gdb.Create(&model.FilmSourcePlaylist{Mid: mid, SourceId: sourceID, LineKind: "play", GroupIndex: group, Content: "[]"})
	}
	play(1, "src_a", 0)
	play(1, "src_a", 1)
	gdb.Create(&model.FilmSourcePlaylist{Mid: 1, SourceId: "src_a", LineKind: "download", GroupIndex: 0, Content: "[]"})
	play(2, "src_b", 0)
	play(3, "src_a", 0)
	play(3, "src_b", 0)
	play(5, "src_a", 0)
	play(99, "src_a", 0)

	gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电影", StableKey: "movie", Show: true, Sort: 1})
	gdb.Create(&model.Category{Id: 2, Pid: 0, Name: "隐藏", StableKey: "hidden", Show: true, Sort: 2})
	gdb.Model(&model.Category{}).Where("id = ?", int64(2)).Update("show", false)
	gdb.Create(&model.Category{Id: 3, Pid: 0, Name: "未映射", StableKey: "loose", Show: true, Sort: 3})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 1, CategoryId: 1})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 2, CategoryId: 2})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 9, CategoryId: 999})
	gdb.Create(&model.CategoryMapping{SourceId: "src_b", SourceTypeId: 1, CategoryId: 1})

	gdb.Create(&model.FailureRecord{OriginId: "src_a", OriginName: "首选站A", Cause: "a"})
	gdb.Create(&model.FailureRecord{OriginId: "src_b", OriginName: "备用站B", Cause: "b"})
	gdb.Create(&model.FailureRecord{OriginId: "src_b", OriginName: "备用站B", Cause: "b2"})
	gdb.Create(&model.FailureRecord{OriginId: "gone", OriginName: "已删站", Cause: "gone"})

	stats := GetInventoryStats()
	if stats.Library.Films != 4 || stats.Library.Playable != 3 || stats.Library.Categories != 3 || stats.Library.Failures != 4 {
		t.Fatalf("library = %+v", stats.Library)
	}
	if len(stats.Sources) != 3 {
		t.Fatalf("expected 3 sources, got %+v", stats.Sources)
	}
	byID := map[string]SourceInventory{}
	for _, item := range stats.Sources {
		byID[item.Id] = item
	}
	primary := byID["src_a"]
	if !primary.IsPrimary || !primary.Enabled || primary.Films != 2 || primary.Categories != 2 || primary.Failures != 1 {
		t.Fatalf("src_a = %+v", primary)
	}
	other := byID["src_b"]
	if other.IsPrimary || other.Films != 2 || other.Categories != 1 || other.Failures != 2 {
		t.Fatalf("src_b = %+v", other)
	}
	off := byID["src_off"]
	if off.Enabled || off.IsPrimary || off.Films != 0 || off.Categories != 0 || off.Failures != 0 {
		t.Fatalf("src_off = %+v", off)
	}
}
