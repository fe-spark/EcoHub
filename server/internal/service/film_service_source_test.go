package service

import (
	"errors"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	filmshared "server/internal/repository/film/shared"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/repository/support"
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

func TestFilmService_GetClassTreeScope(t *testing.T) {
	gdb := setupFilmServiceTestDB(t)
	gdb.Create(&model.FilmSource{Id: "src_b", Name: "站点B", Uri: "http://b", State: true, Sort: 2})
	gdb.Create(&model.FilmSource{Id: "src_a", Name: "站点A", Uri: "http://a", State: true, Sort: 0})
	gdb.Create(&model.Category{Id: 9, Pid: 0, Name: "电影", StableKey: "a-movie", Show: true, Sort: 1})
	gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电视剧", StableKey: "b-tv", Show: true, Sort: 1})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 1, CategoryId: 9})
	gdb.Create(&model.CategoryMapping{SourceId: "src_b", SourceTypeId: 2, CategoryId: 1})
	gdb.Create(&model.SourceCategory{SourceId: "src_a", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 1, Depth: 0})
	gdb.Create(&model.SourceCategory{SourceId: "src_b", SourceTypeId: 2, ParentSourceTypeId: 0, RawName: "电视剧", Sort: 1, Depth: 0})

	srv := new(FilmService)
	view, err := srv.GetClassTreeScope("")
	if err != nil {
		t.Fatal(err)
	}
	if view.DefaultSourceId != "src_a" || view.CurrentSourceId != "src_a" || view.PrimarySourceId != "src_a" {
		t.Fatalf("default=%s current=%s primary=%s", view.DefaultSourceId, view.CurrentSourceId, view.PrimarySourceId)
	}
	if len(view.Sources) != 2 {
		t.Fatalf("sources = %d", len(view.Sources))
	}
	if len(view.Tree.Children) != 1 || view.Tree.Children[0].Name != "电影" {
		t.Fatalf("primary tree = %+v", view.Tree.Children)
	}

	peerView, err := srv.GetClassTreeScope("src_b")
	if err != nil {
		t.Fatal(err)
	}
	if peerView.CurrentSourceId != "src_b" || peerView.PrimarySourceId != "src_a" {
		t.Fatalf("peer current=%s primary=%s", peerView.CurrentSourceId, peerView.PrimarySourceId)
	}
	if len(peerView.Tree.Children) != 1 || peerView.Tree.Children[0].Name != "电视剧" {
		t.Fatalf("peer tree = %+v", peerView.Tree.Children)
	}

	if _, err := srv.GetClassTreeScope("missing"); err == nil {
		t.Fatal("expected error for unknown source")
	}
}

func TestFilmService_SaveSourceClassTree_BySource(t *testing.T) {
	gdb := setupFilmServiceTestDB(t)
	gdb.Create(&model.FilmSource{Id: "src_a", Name: "站点A", Uri: "http://a", State: true, Sort: 0})
	gdb.Create(&model.FilmSource{Id: "src_b", Name: "站点B", Uri: "http://b", State: true, Sort: 1})
	gdb.Create(&model.Category{Id: 9, Pid: 0, Name: "电影", StableKey: "source:src_a:1", Show: true, Sort: 1})
	gdb.Create(&model.Category{Id: 10, Pid: 0, Name: "电视剧", StableKey: "source:src_b:2", Show: true, Sort: 1})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 1, CategoryId: 9})
	gdb.Create(&model.CategoryMapping{SourceId: "src_b", SourceTypeId: 2, CategoryId: 10})
	gdb.Create(&model.SourceCategory{SourceId: "src_a", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 1, Depth: 0})
	gdb.Create(&model.SourceCategory{SourceId: "src_b", SourceTypeId: 2, ParentSourceTypeId: 0, RawName: "电视剧", Sort: 1, Depth: 0})

	srv := new(FilmService)
	if err := srv.SaveSourceClassTree("src_b", []*model.CategoryTree{{Id: 10, Name: "电视剧", Show: false}}); err != nil {
		t.Fatalf("save to non-primary should succeed, got %v", err)
	}
	if err := srv.SaveSourceClassTree("", []*model.CategoryTree{{Id: 9, Name: "电影", Show: true}}); err != nil {
		t.Fatalf("save with empty source should use primary: %v", err)
	}
	if err := srv.SaveSourceClassTree("missing", []*model.CategoryTree{{Id: 9, Name: "电影", Show: true}}); err == nil {
		t.Fatal("save to missing source should fail")
	}

	var catA, catB model.Category
	gdb.First(&catA, 9)
	gdb.First(&catB, 10)
	if !catA.Show || catB.Show {
		t.Fatalf("expected catA.Show=true catB.Show=false, got %v %v", catA.Show, catB.Show)
	}
}

func TestGetFilmDetail_ExcludesHiddenNonPrimaryCategoryPeerPlay(t *testing.T) {
	gdb, _ := setupTestDBAndRedis(t)
	t.Cleanup(func() { support.SetCategoryShowForTest(map[int64]bool{}) })

	gdb.Create(&model.FilmSource{Id: "src_a", Name: "主站A", Uri: "http://a", State: true, Sort: 0})
	gdb.Create(&model.FilmSource{Id: "src_b", Name: "备站B", Uri: "http://b", State: true, Sort: 1})
	gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电影", StableKey: "source:src_a:1", Show: true, Sort: 1})
	gdb.Create(&model.Category{Id: 20, Pid: 0, Name: "伦理", StableKey: "source:src_b:2", Show: true, Sort: 1})
	gdb.Create(&model.Category{Id: 21, Pid: 20, Name: "福利", StableKey: "source:src_b:12", Show: true, Sort: 1})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 1, CategoryId: 1})
	gdb.Create(&model.CategoryMapping{SourceId: "src_b", SourceTypeId: 2, CategoryId: 20})
	gdb.Create(&model.CategoryMapping{SourceId: "src_b", SourceTypeId: 12, CategoryId: 21})
	gdb.Create(&model.SourceCategory{SourceId: "src_a", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 1, Depth: 0})
	gdb.Create(&model.SourceCategory{SourceId: "src_b", SourceTypeId: 2, ParentSourceTypeId: 0, RawName: "伦理", Sort: 1, Depth: 0})
	gdb.Create(&model.SourceCategory{SourceId: "src_b", SourceTypeId: 12, ParentSourceTypeId: 2, RawName: "福利", Sort: 1, Depth: 1})
	repository.MarkCategoryChanged()

	const version = "live"
	filmA := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 101, FirstSourceId: "src_a", DbId: 9001},
		FilmIndexCategory: model.FilmIndexCategory{Cid: 1, Pid: 1, CategoryKey: "source:src_a:1", RootCategoryKey: "source:src_a:1"},
		FilmIndexContent:  model.FilmIndexContent{Name: "测试影片"},
	}
	filmB := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 102, FirstSourceId: "src_b", DbId: 9001},
		FilmIndexCategory: model.FilmIndexCategory{Cid: 21, Pid: 20, CategoryKey: "source:src_b:12", RootCategoryKey: "source:src_b:2"},
		FilmIndexContent:  model.FilmIndexContent{Name: "测试影片"},
	}
	gdb.Create(&filmA)
	gdb.Create(&filmB)
	for _, k := range filmshared.BuildMovieMatchKeysWithCategory(9001, "测试影片", 1) {
		gdb.Create(&model.MovieMatchKey{Mid: 101, MatchKey: k})
	}
	for _, k := range filmshared.BuildMovieMatchKeysWithCategory(9001, "测试影片", 20) {
		gdb.Create(&model.MovieMatchKey{Mid: 102, MatchKey: k})
	}
	gdb.Create(&model.FilmSourcePlaylist{Mid: 101, SourceId: "src_a", LineKind: model.LineKindPlay, GroupIndex: 0, Content: `[{"episode":"1","link":"http://a/1"}]`})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 102, SourceId: "src_b", LineKind: model.LineKindPlay, GroupIndex: 0, Content: `[{"episode":"1","link":"http://b/1"}]`})

	_ = filmsnapshot.SetActiveSnapshotVersion(version)
	_ = filmsnapshot.LoadActiveFilmReadModel(version)
	filmsnapshot.WaitActiveFilmSearchIndexBuilt()

	// 1. 未隐藏前，主站详情和 TVBox 均能补充非主站播放源
	before, err := IndexSvc.GetFilmDetail(101)
	if err != nil {
		t.Fatalf("GetFilmDetail before hide: %v", err)
	}
	if len(before.List) != 2 {
		t.Fatalf("expected 2 play sources before hide, got %+v", before.List)
	}

	// 2. 在分类管理中隐藏非主站 src_b 的二级分类「福利」
	if err := new(FilmService).SaveSourceClassTree("src_b", []*model.CategoryTree{
		{
			Id:   20,
			Name: "伦理",
			Show: true,
			Children: []*model.CategoryTree{
				{Id: 21, Name: "福利", Show: false},
			},
		},
	}); err != nil {
		t.Fatalf("SaveSourceClassTree: %v", err)
	}

	// 3. 缓存被清理，主站详情与 TVBox 批量读取均不得再出现 src_b 播放源
	after, err := IndexSvc.GetFilmDetail(101)
	if err != nil {
		t.Fatalf("GetFilmDetail after hide: %v", err)
	}
	if len(after.List) != 1 || after.List[0].SourceId != "src_a" {
		t.Fatalf("expected only src_a play source after hiding child category, got %+v", after.List)
	}
	batch, _, err := BatchGetPlayPlaylistsByMids([]int64{101})
	if err != nil {
		t.Fatalf("BatchGetPlayPlaylistsByMids: %v", err)
	}
	if len(batch[101]) != 1 || batch[101][0].SourceId != "src_a" {
		t.Fatalf("expected TVBox batch to exclude hidden peer source, got %+v", batch[101])
	}

	// 4. 恢复二级显示但隐藏一级父分类「伦理」，同样不得出现
	if err := new(FilmService).SaveSourceClassTree("src_b", []*model.CategoryTree{
		{
			Id:   20,
			Name: "伦理",
			Show: false,
			Children: []*model.CategoryTree{
				{Id: 21, Name: "福利", Show: true},
			},
		},
	}); err != nil {
		t.Fatalf("SaveSourceClassTree hide root: %v", err)
	}
	afterRootHide, err := IndexSvc.GetFilmDetail(101)
	if err != nil {
		t.Fatalf("GetFilmDetail after root hide: %v", err)
	}
	if len(afterRootHide.List) != 1 || afterRootHide.List[0].SourceId != "src_a" {
		t.Fatalf("expected only src_a play source after hiding root category, got %+v", afterRootHide.List)
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
