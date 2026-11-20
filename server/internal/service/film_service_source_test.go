package service

import (
	"errors"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupFilmServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := testutil.MemDSN(t)
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
		&model.SourceCategory{},
		&model.CategoryMapping{},
		&model.MovieSourceMapping{}, // 按站筛选会联表 movie_source_mapping
		&model.SiteConfigRecord{},
		&model.PrimaryState{},
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

func TestFilmService_GetSearchOptions_PrimaryOnly(t *testing.T) {
	gdb := setupFilmServiceTestDB(t)

	// 创建两个启用的采集站，sort 决定主站懒初始化顺序
	gdb.Create(&model.FilmSource{Id: "src_b", Name: "站点B", Uri: "http://b.com", State: true, Sort: 2})
	gdb.Create(&model.FilmSource{Id: "src_a", Name: "站点A", Uri: "http://a.com", State: true, Sort: 1})

	srv := new(FilmService)
	options, err := srv.GetSearchOptions()
	if err != nil {
		t.Fatal(err)
	}

	// X-12：只返回主站 ID 与分类树，不再暴露站点列表与默认站点
	if primary, ok := options["primarySourceId"].(string); !ok || primary != "src_a" {
		t.Fatalf("expected primarySourceId to be src_a, got %v", options["primarySourceId"])
	}
	for _, key := range []string{"sources", "defaultSourceId", "currentSourceId"} {
		if _, ok := options[key]; ok {
			t.Fatalf("options must not expose %q (X-12)", key)
		}
	}
}

func TestFilmService_GetPrimaryClassTree(t *testing.T) {
	gdb := setupFilmServiceTestDB(t)
	gdb.Create(&model.FilmSource{Id: "src_b", Name: "站点B", Uri: "http://b", State: true, Sort: 2})
	gdb.Create(&model.FilmSource{Id: "src_a", Name: "站点A", Uri: "http://a", State: true, Sort: 0})
	gdb.Create(&model.Category{Id: 9, Pid: 0, Name: "电影", StableKey: "a-movie", Show: true, Sort: 1})
	gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电视剧", StableKey: "b-tv", Show: true, Sort: 1})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 1, CategoryId: 9})
	gdb.Create(&model.CategoryMapping{SourceId: "src_b", SourceTypeId: 2, CategoryId: 1})
	gdb.Create(&model.SourceCategory{SourceId: "src_a", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 1, Depth: 0})
	gdb.Create(&model.SourceCategory{SourceId: "src_b", SourceTypeId: 2, ParentSourceTypeId: 0, RawName: "电视剧", Sort: 1, Depth: 0})

	view, err := new(FilmService).GetPrimaryClassTree()
	if err != nil {
		t.Fatal(err)
	}
	if view.DefaultSourceId != "src_a" || view.CurrentSourceId != "src_a" {
		t.Fatalf("default source = %s current = %s", view.DefaultSourceId, view.CurrentSourceId)
	}
	// Sources 保留给分类规则页做采集侧站点选择（D2）
	if len(view.Sources) != 2 {
		t.Fatalf("sources = %d", len(view.Sources))
	}
	// 展示树固定主站：始终是 src_a 的分类
	if len(view.Tree.Children) != 1 || view.Tree.Children[0].Name != "电影" {
		t.Fatalf("primary tree = %+v", view.Tree.Children)
	}
}

// D3：展示树写请求只允许主站；空 sourceId 视为主站。
func TestFilmService_SaveSourceClassTree_PrimaryOnly(t *testing.T) {
	gdb := setupFilmServiceTestDB(t)
	gdb.Create(&model.FilmSource{Id: "src_a", Name: "站点A", Uri: "http://a", State: true, Sort: 0})
	gdb.Create(&model.FilmSource{Id: "src_b", Name: "站点B", Uri: "http://b", State: true, Sort: 1})
	gdb.Create(&model.Category{Id: 9, Pid: 0, Name: "电影", StableKey: "a-movie", Show: true, Sort: 1})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 1, CategoryId: 9})
	gdb.Create(&model.SourceCategory{SourceId: "src_a", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 1, Depth: 0})

	srv := new(FilmService)
	// 树节点 ID 为展示分类 ID
	nodes := []*model.CategoryTree{{Id: 9, Name: "电影", Show: true}}
	if err := srv.SaveSourceClassTree("src_b", nodes); !errors.Is(err, ErrPrimaryMismatch) {
		t.Fatalf("save to non-primary should mismatch, got %v", err)
	}
	if err := srv.SaveSourceClassTree("", nodes); err != nil {
		t.Fatalf("save with empty source should use primary: %v", err)
	}
}

func TestValidatePrimaryFilmIDs(t *testing.T) {
	gdb := setupFilmServiceTestDB(t)
	gdb.Create(&model.FilmSource{Id: "src1", Name: "主站", State: true, Sort: 0})
	gdb.Create(&model.FilmSource{Id: "src2", Name: "副站", State: true, Sort: 1})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 301, FirstSourceId: "src1"},
		FilmIndexContent:  model.FilmIndexContent{Name: "主站影片"},
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 302, FirstSourceId: "src2"},
		FilmIndexContent:  model.FilmIndexContent{Name: "副站影片"},
	})

	if _, err := validatePrimaryFilmIDs([]int64{301}, "src1"); err != nil {
		t.Fatalf("primary-owned film should pass: %v", err)
	}
	if _, err := validatePrimaryFilmIDs([]int64{301}, "src2"); !errors.Is(err, ErrPrimaryMismatch) {
		t.Fatalf("stale source context should mismatch, got %v", err)
	}
	if _, err := validatePrimaryFilmIDs([]int64{302}, ""); !errors.Is(err, ErrPrimaryMismatch) {
		t.Fatalf("non-primary film id should mismatch, got %v", err)
	}
}

func TestFilmService_GetFilmPage_FixedPrimary(t *testing.T) {
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

	// 1. 未指定 SourceId 时固定主站 src1
	page1, err := srv.GetFilmPage(&model.SearchVo{
		Paging: &dto.Page{Current: 1, PageSize: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 1 || page1[0].Mid != 101 {
		t.Fatalf("expected 1 film from primary src1 (mid=101), got %v", page1)
	}

	// 2. D3：携带非主站 sourceId 被忽略，仍返回主站数据
	page2, err := srv.GetFilmPage(&model.SearchVo{
		SourceId: "src2",
		Paging:   &dto.Page{Current: 1, PageSize: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 1 || page2[0].Mid != 101 {
		t.Fatalf("sourceId must be ignored in favor of primary, got %v", page2)
	}
}

func TestFilmService_GetFilmPage_SnapshotMode_FixedPrimary(t *testing.T) {
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

	// 未带 SourceId -> 主站 src1
	page1, err := srv.GetFilmPage(&model.SearchVo{
		Paging: &dto.Page{Current: 1, PageSize: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 1 || page1[0].Mid != 201 {
		t.Fatalf("expected 1 film from primary src1 in snapshot (mid=201), got %v", page1)
	}

	// D3：非主站 sourceId 被忽略 -> 仍是主站影片
	page2, err := srv.GetFilmPage(&model.SearchVo{
		SourceId: "src2",
		Paging:   &dto.Page{Current: 1, PageSize: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 1 || page2[0].Mid != 201 {
		t.Fatalf("sourceId must be ignored in favor of primary, got %v", page2)
	}
}

func TestFilmService_DeletePrimaryFilms_AllowsDuringSearchWarmAndChecksOwnership(t *testing.T) {
	gdb := setupFilmServiceTestDB(t)
	if err := gdb.AutoMigrate(model.AllModels...); err != nil {
		t.Fatal(err)
	}
	gdb.Create(&model.FilmSource{Id: "src1", Name: "源1", State: true, Sort: 0})
	gdb.Create(&model.FilmSource{Id: "src2", Name: "源2", State: true, Sort: 1})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 301, FirstSourceId: "src1"},
		FilmIndexContent:  model.FilmIndexContent{Name: "主站影片"},
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 302, FirstSourceId: "src2"},
		FilmIndexContent:  model.FilmIndexContent{Name: "非主站影片"},
	})

	// 模拟正在预热：SwitchPrimary 会因 ErrSearchIndexBusy 被拒，但删片不应误报切主错误
	primaryWarmCount.Add(1)
	defer primaryWarmCount.Add(-1)

	srv := new(FilmService)
	if err := srv.DeletePrimaryFilms([]int64{302}, "src1"); err != ErrPrimaryMismatch {
		t.Fatalf("expected ErrPrimaryMismatch for non-primary film, got %v", err)
	}
	if err := srv.DeletePrimaryFilms([]int64{301}, "src1"); err != nil {
		t.Fatalf("DeletePrimaryFilms must succeed during search warm, got %v", err)
	}

	var count int64
	gdb.Unscoped().Model(&model.FilmIndex{}).Where("mid = ?", 301).Count(&count)
	if count != 0 {
		t.Fatalf("expected film 301 deleted, count=%d", count)
	}
}

func TestFilmService_SaveFilmDetail_RejectsNonPrimaryMid(t *testing.T) {
	gdb := setupFilmServiceTestDB(t)
	if err := gdb.AutoMigrate(model.AllModels...); err != nil {
		t.Fatal(err)
	}
	gdb.Create(&model.FilmSource{Id: "src1", Name: "主站", State: true, Sort: 0})
	gdb.Create(&model.FilmSource{Id: "src2", Name: "非主站", State: true, Sort: 1})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 402, FirstSourceId: "src2"},
		FilmIndexContent:  model.FilmIndexContent{Name: "非主站原片名"},
	})
	_ = repository.RefreshCollectSourceCatalog()

	srv := new(FilmService)
	err := srv.SaveFilmDetail(model.FilmDetailVo{
		Id:   402,
		Name: "试图跨站篡改",
		Pid:  1,
		Cid:  2,
	})
	if err != ErrPrimaryMismatch {
		t.Fatalf("expected ErrPrimaryMismatch when saving non-primary mid, got %v", err)
	}

	var saved model.FilmIndex
	if err := gdb.First(&saved, "mid = ?", 402).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Name != "非主站原片名" || saved.FirstSourceId != "src2" {
		t.Fatalf("non-primary film must remain untouched, got %+v", saved)
	}
}
