package service

import (
	"fmt"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	filmsnapshot "server/internal/repository/film/snapshot"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupFilmServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
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
	); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	origMdb := db.Mdb
	origRdb := db.Rdb
	db.Mdb = gdb
	db.Rdb = nil
	filmsnapshot.ClearActiveFilmReadModel()
	filmsnapshot.ClearActiveSnapshotVersion()

	t.Cleanup(func() {
		db.Mdb = origMdb
		db.Rdb = origRdb
		filmsnapshot.ClearActiveFilmReadModel()
		filmsnapshot.ClearActiveSnapshotVersion()
	})
	return gdb
}

func TestFilmService_GetSearchOptions_DefaultSource(t *testing.T) {
	gdb := setupFilmServiceTestDB(t)

	// 创建两个启用的采集站，sort 决定顺序
	gdb.Create(&model.FilmSource{Id: "src_b", Name: "站点B", Uri: "http://b.com", State: true, Sort: 2})
	gdb.Create(&model.FilmSource{Id: "src_a", Name: "站点A", Uri: "http://a.com", State: true, Sort: 1})

	srv := new(FilmService)
	options := srv.GetSearchOptions()

	if defaultSrc, ok := options["defaultSourceId"].(string); !ok || defaultSrc != "src_a" {
		t.Fatalf("expected defaultSourceId to be src_a, got %v", options["defaultSourceId"])
	}
	if currSrc, ok := options["currentSourceId"].(string); !ok || currSrc != "src_a" {
		t.Fatalf("expected currentSourceId to be src_a, got %v", options["currentSourceId"])
	}

	sources, ok := options["sources"].([]model.FilmSource)
	if !ok || len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %v", options["sources"])
	}
	if sources[0].Id != "src_a" {
		t.Fatalf("expected first source in list to be src_a (sort=1), got %s", sources[0].Id)
	}
}

func TestFilmService_GetScopedClassTree_DefaultsToPrimary(t *testing.T) {
	gdb := setupFilmServiceTestDB(t)
	gdb.Create(&model.FilmSource{Id: "src_b", Name: "站点B", Uri: "http://b", State: true, Sort: 2})
	gdb.Create(&model.FilmSource{Id: "src_a", Name: "站点A", Uri: "http://a", State: true, Sort: 0})
	gdb.Create(&model.Category{Id: 9, Pid: 0, Name: "电影", StableKey: "a-movie", Show: true, Sort: 1})
	gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电视剧", StableKey: "b-tv", Show: true, Sort: 1})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 1, CategoryId: 9})
	gdb.Create(&model.CategoryMapping{SourceId: "src_b", SourceTypeId: 2, CategoryId: 1})

	view := new(FilmService).GetScopedClassTree("")
	if view.DefaultSourceId != "src_a" || view.CurrentSourceId != "src_a" {
		t.Fatalf("default source = %s current = %s", view.DefaultSourceId, view.CurrentSourceId)
	}
	if len(view.Sources) != 2 {
		t.Fatalf("sources = %d", len(view.Sources))
	}
	if len(view.Tree.Children) != 1 || view.Tree.Children[0].Name != "电影" {
		t.Fatalf("default tree = %+v", view.Tree.Children)
	}
	picked := new(FilmService).GetScopedClassTree("src_b")
	if len(picked.Tree.Children) != 1 || picked.Tree.Children[0].Name != "电视剧" {
		t.Fatalf("src_b tree = %+v", picked.Tree.Children)
	}
}

func TestFilmService_GetFilmPage_FilterBySource(t *testing.T) {
	gdb := setupFilmServiceTestDB(t)

	// 创建采集源
	gdb.Create(&model.FilmSource{Id: "src1", Name: "源1", State: true, Sort: 1})
	gdb.Create(&model.FilmSource{Id: "src2", Name: "源2", State: true, Sort: 2})

	// 创建影片 1 (属于 src1), 影片 2 (属于 src2)
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 101, FirstSourceId: "src1"},
		FilmIndexContent:  model.FilmIndexContent{Name: "源1影片"},
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 102, FirstSourceId: "src2"},
		FilmIndexContent:  model.FilmIndexContent{Name: "源2影片"},
	})

	gdb.Create(&model.FilmSourcePlaylist{Mid: 101, SourceId: "src1", LineKind: "play", Content: "[]"})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 102, SourceId: "src2", LineKind: "play", Content: "[]"})

	srv := new(FilmService)

	// 1. 未指定 SourceId 时，默认筛选第一个源 (src1)
	page1 := srv.GetFilmPage(model.SearchVo{
		Paging: &dto.Page{Current: 1, PageSize: 10},
	})
	if len(page1) != 1 || page1[0].Mid != 101 {
		t.Fatalf("expected 1 film from default src1 (mid=101), got %v", page1)
	}

	// 2. 指定 SourceId = src2 时，筛选出 src2 的影片
	page2 := srv.GetFilmPage(model.SearchVo{
		SourceId: "src2",
		Paging:   &dto.Page{Current: 1, PageSize: 10},
	})
	if len(page2) != 1 || page2[0].Mid != 102 {
		t.Fatalf("expected 1 film from src2 (mid=102), got %v", page2)
	}
}

func TestFilmService_GetFilmPage_SnapshotMode_FilterBySource(t *testing.T) {
	gdb := setupFilmServiceTestDB(t)

	snapVer := "snap_test_source_ver"
	_ = filmsnapshot.SetActiveSnapshotVersion(snapVer)

	gdb.Create(&model.FilmSource{Id: "src1", Name: "源1", State: true, Sort: 1})
	gdb.Create(&model.FilmSource{Id: "src2", Name: "源2", State: true, Sort: 2})

	_ = filmsnapshot.WriteLiveFilmsFromSnapshots([]model.FilmListSnapshot{
		{Mid: 201, SourceId: "src1", Name: "快照影片1"},
		{Mid: 202, SourceId: "src2", Name: "快照影片2"},
	})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 201, SourceId: "src1", LineKind: "play"})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 202, SourceId: "src2", LineKind: "play"})

	srv := new(FilmService)

	// 默认未带 SourceId -> 首位源 src1
	page1 := srv.GetFilmPage(model.SearchVo{
		Paging: &dto.Page{Current: 1, PageSize: 10},
	})
	if len(page1) != 1 || page1[0].Mid != 201 {
		t.Fatalf("expected 1 film from default src1 in snapshot (mid=201), got %v", page1)
	}

	// 筛选 src2 -> 快照影片2
	page2 := srv.GetFilmPage(model.SearchVo{
		SourceId: "src2",
		Paging:   &dto.Page{Current: 1, PageSize: 10},
	})
	if len(page2) != 1 || page2[0].Mid != 202 {
		t.Fatalf("expected 1 film from src2 in snapshot (mid=202), got %v", page2)
	}
}
