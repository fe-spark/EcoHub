package film

import (
	"errors"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/film/catalog"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/film/snapshot"
	"server/internal/repobase"
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
	snapshot.ClearActiveFilmReadModel()
	snapshot.ClearActiveSnapshotVersion()

	t.Cleanup(func() {
		db.Mdb = origMdb
		db.Rdb = origRdb
		snapshot.ClearActiveFilmReadModel()
		snapshot.ClearActiveSnapshotVersion()
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

func TestFilmService_GetClassTreeScope_WithFilmCounts(t *testing.T) {
	gdb := setupFilmServiceTestDB(t)
	gdb.Create(&model.FilmSource{Id: "src_a", Name: "站点A", Uri: "http://a", State: true, Sort: 0})
	gdb.Create(&model.FilmSource{Id: "src_b", Name: "站点B", Uri: "http://b", State: true, Sort: 1})
	gdb.Create(&model.Category{Id: 9, Pid: 0, Name: "电影", StableKey: "a-movie", Show: true, Sort: 1})
	gdb.Create(&model.Category{Id: 10, Pid: 9, Name: "动作片", StableKey: "a-action", Show: true, Sort: 1})
	gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电视剧", StableKey: "b-tv", Show: true, Sort: 1})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 1, CategoryId: 9})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 6, CategoryId: 10})
	gdb.Create(&model.CategoryMapping{SourceId: "src_b", SourceTypeId: 2, CategoryId: 1})
	gdb.Create(&model.SourceCategory{SourceId: "src_a", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 1, Depth: 0})
	gdb.Create(&model.SourceCategory{SourceId: "src_a", SourceTypeId: 6, ParentSourceTypeId: 1, RawName: "动作片", Sort: 1, Depth: 1})
	gdb.Create(&model.SourceCategory{SourceId: "src_b", SourceTypeId: 2, ParentSourceTypeId: 0, RawName: "电视剧", Sort: 1, Depth: 0})
	gdb.Create([]model.FilmIndex{
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 101, FirstSourceId: "src_a"}, FilmIndexCategory: model.FilmIndexCategory{CategoryKey: "source:src_a:6"}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 102, FirstSourceId: "src_a"}, FilmIndexCategory: model.FilmIndexCategory{CategoryKey: "source:src_a:1"}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 103, FirstSourceId: "src_b"}, FilmIndexCategory: model.FilmIndexCategory{CategoryKey: "source:src_b:2"}},
	})
	gdb.Create([]model.FilmSourcePlaylist{
		{Mid: 101, SourceId: "src_a", LineKind: model.LineKindPlay, GroupIndex: 0, Content: `[{"episode":"1","link":"http://a/101"}]`},
		{Mid: 102, SourceId: "src_a", LineKind: model.LineKindPlay, GroupIndex: 0, Content: `[{"episode":"1","link":"http://a/102"}]`},
		{Mid: 103, SourceId: "src_b", LineKind: model.LineKindPlay, GroupIndex: 0, Content: `[{"episode":"1","link":"http://b/103"}]`},
	})
	_ = snapshot.SetActiveSnapshotVersion("live")
	if err := snapshot.LoadActiveFilmReadModel("live"); err != nil {
		t.Fatalf("LoadActiveFilmReadModel: %v", err)
	}
	snapshot.WaitActiveFilmSearchIndexBuilt()
	// 封口后再软删除主站 1 条库记录，验证主站直接命中内存快照（仍为 2），而非查库。
	if err := gdb.Where("mid = ?", 102).Delete(&model.FilmIndex{}).Error; err != nil {
		t.Fatalf("soft delete primary film: %v", err)
	}

	srv := new(FilmService)
	viewA, err := srv.GetClassTreeScope("")
	if err != nil {
		t.Fatalf("GetClassTreeScope primary: %v", err)
	}
	if len(viewA.Tree.Children) != 1 || viewA.Tree.Children[0].FilmCount != 2 {
		t.Fatalf("primary root count mismatch (expected memory snapshot 2): %+v", viewA.Tree.Children)
	}
	if len(viewA.Tree.Children[0].Children) != 1 || viewA.Tree.Children[0].Children[0].FilmCount != 1 {
		t.Fatalf("primary child count mismatch: %+v", viewA.Tree.Children[0].Children)
	}

	viewB, err := srv.GetClassTreeScope("src_b")
	if err != nil {
		t.Fatalf("GetClassTreeScope src_b: %v", err)
	}
	if len(viewB.Tree.Children) != 1 || viewB.Tree.Children[0].FilmCount != 1 {
		t.Fatalf("peer root count mismatch: %+v", viewB.Tree.Children)
	}
}

func TestFilmService_GetClassTreeScope_CollectingAndEmptySnapshotFallbackToDB(t *testing.T) {
	gdb := setupFilmServiceTestDB(t)
	gdb.Create(&model.FilmSource{Id: "src_a", Name: "站点A", Uri: "http://a", State: true, Sort: 0})
	gdb.Create(&model.Category{Id: 9, Pid: 0, Name: "电影", StableKey: "a-movie", Show: true, Sort: 1})
	gdb.Create(&model.CategoryMapping{SourceId: "src_a", SourceTypeId: 1, CategoryId: 9})
	gdb.Create(&model.SourceCategory{SourceId: "src_a", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 1, Depth: 0})

	// 1. 库为空时封口一次，模拟刚启动/建站时的空检索快照，随后写入新影片但尚未重封口。
	_ = snapshot.SetActiveSnapshotVersion("live")
	if err := snapshot.LoadActiveFilmReadModel("live"); err != nil {
		t.Fatalf("LoadActiveFilmReadModel empty: %v", err)
	}
	snapshot.WaitActiveFilmSearchIndexBuilt()
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 201, FirstSourceId: "src_a"},
		FilmIndexCategory: model.FilmIndexCategory{CategoryKey: "source:src_a:1"},
	})
	gdb.Create(&model.FilmSourcePlaylist{
		Mid: 201, SourceId: "src_a", LineKind: model.LineKindPlay, GroupIndex: 0, Content: `[{"episode":"1","link":"http://a/201"}]`,
	})

	srv := new(FilmService)
	scope, err := srv.GetClassTreeScope("")
	if err != nil {
		t.Fatalf("GetClassTreeScope empty snapshot fallback: %v", err)
	}
	if len(scope.Tree.Children) != 1 || scope.Tree.Children[0].FilmCount != 1 {
		t.Fatalf("expected DB count 1 when snapshot is empty, got %+v", scope.Tree.Children)
	}

	// 2. 非空快照状态下若正在采集中，新增入库影片也应实时反映数据库最新数量。
	if err := snapshot.LoadActiveFilmReadModel("live"); err != nil {
		t.Fatalf("LoadActiveFilmReadModel populated: %v", err)
	}
	snapshot.WaitActiveFilmSearchIndexBuilt()
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 202, FirstSourceId: "src_a"},
		FilmIndexCategory: model.FilmIndexCategory{CategoryKey: "source:src_a:1"},
	})
	prevCollecting := IsFilmCollectingFn
	IsFilmCollectingFn = func() bool { return true }
	t.Cleanup(func() {
		IsFilmCollectingFn = prevCollecting
	})

	collectingScope, err := srv.GetClassTreeScope("")
	if err != nil {
		t.Fatalf("GetClassTreeScope while collecting: %v", err)
	}
	if len(collectingScope.Tree.Children) != 1 || collectingScope.Tree.Children[0].FilmCount != 2 {
		t.Fatalf("expected live DB count 2 while collecting, got %+v", collectingScope.Tree.Children)
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
	if _, err := validatePrimaryFilmIDs([]int64{301}, "src2"); !errors.Is(err, repobase.ErrPrimaryMismatch) {
		t.Fatalf("stale source context should mismatch, got %v", err)
	}
	if _, err := validatePrimaryFilmIDs([]int64{302}, ""); !errors.Is(err, repobase.ErrPrimaryMismatch) {
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
	_ = snapshot.SetActiveSnapshotVersion(snapVer)

	gdb.Create(&model.FilmSource{Id: "src1", Name: "源1", State: true, Sort: 1})
	gdb.Create(&model.FilmSource{Id: "src2", Name: "源2", State: true, Sort: 2})

	_ = snapshot.WriteLiveFilmsFromSnapshots([]model.FilmListSnapshot{
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
	snapshot.BeginPrimaryWarmup()
	defer snapshot.EndPrimaryWarmup()

	srv := new(FilmService)
	if err := srv.DeletePrimaryFilms([]int64{302}, "src1"); err != repobase.ErrPrimaryMismatch {
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

	// 已软删除的非主站影片、不存在 ID 或全无效 ID 不得通过主站归属校验，避免后续 Unscoped 硬删越权抹除数据。
	if err := gdb.Where("mid = ?", 302).Delete(&model.FilmIndex{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := srv.DeletePrimaryFilms([]int64{302}, "src1"); err != repobase.ErrPrimaryMismatch {
		t.Fatalf("expected ErrPrimaryMismatch for soft-deleted non-primary film, got %v", err)
	}
	gdb.Unscoped().Model(&model.FilmIndex{}).Where("mid = ?", 302).Count(&count)
	if count != 1 {
		t.Fatalf("soft-deleted non-primary film 302 must remain protected, count=%d", count)
	}
	if err := srv.DeletePrimaryFilms([]int64{999999}, "src1"); err != repobase.ErrPrimaryMismatch {
		t.Fatalf("expected ErrPrimaryMismatch for nonexistent film, got %v", err)
	}
	if err := srv.DeletePrimaryFilms([]int64{0, -1}, "src1"); err != repobase.ErrPrimaryMismatch {
		t.Fatalf("expected ErrPrimaryMismatch for invalid ids, got %v", err)
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
	_ = repobase.RefreshCollectSourceCatalog()

	srv := new(FilmService)
	err := srv.SaveFilmDetail(model.FilmDetailVo{
		Id:   402,
		Name: "试图跨站篡改",
		Pid:  1,
		Cid:  2,
	})
	if err != repobase.ErrPrimaryMismatch {
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

func TestFilmService_ManualSourceMid_UniqueNegative(t *testing.T) {
	now := time.Unix(1700000000, 123456)
	seen := make(map[int64]struct{}, 50)
	for i := 0; i < 50; i++ {
		id := nextManualSourceMid(now)
		if id >= 0 {
			t.Fatalf("expected negative manual source_mid, got %d", id)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate manual source_mid generated at same timestamp: %d", id)
		}
		seen[id] = struct{}{}
	}
}

func TestFilmService_AggregatedPrimaryFilmAllowedAndManualLineClear(t *testing.T) {
	gdb := setupFilmServiceTestDB(t)
	if err := gdb.AutoMigrate(model.AllModels...); err != nil {
		t.Fatal(err)
	}
	gdb.Create(&model.FilmSource{Id: "src1", Name: "主站", State: true, Sort: 0})
	gdb.Create(&model.FilmSource{Id: "src2", Name: "副站", State: true, Sort: 1})
	gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电影", Show: true, Sort: 1})
	gdb.Create(&model.CategoryMapping{SourceId: "src1", SourceTypeId: 1, CategoryId: 1})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 501, FirstSourceId: "src2"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1, Cid: 1, CName: "电影", CategoryKey: "source:src2:99", RootCategoryKey: "source:src2:88"},
		FilmIndexContent:  model.FilmIndexContent{Name: "聚合影片"},
	})
	gdb.Create(&model.FilmSourcePlaylist{
		Mid: 501, SourceId: "src1", LineKind: model.LineKindPlay, GroupIndex: 0, GroupName: "主站线路",
		Content: `[{"episode":"第1集","link":"https://example.com/1.m3u8"}]`,
	})
	_ = repobase.RefreshCollectSourceCatalog()
	repobase.ReloadMappingRules()

	// 聚合影片（首采在 src2 但挂载了 src1 播放线路）在主站 src1 下应允许操作
	if _, err := validatePrimaryFilmIDs([]int64{501}, "src1"); err != nil {
		t.Fatalf("aggregated primary film should pass ownership check: %v", err)
	}

	srv := new(FilmService)
	// 后台详情读取：跨站聚合影片不应回传非主站的 RawCid/RawPid，避免在主站分类树误匹配
	aggDetail, aggManual, err := srv.GetStoredFilmDetail(501)
	if err != nil {
		t.Fatalf("get stored aggregated film detail failed: %v", err)
	}
	if aggManual || aggDetail.RawCid != 0 || aggDetail.RawPid != 0 {
		t.Fatalf("expected non-primary aggregated film RawCid=0 RawPid=0 manual=false, got rawCid=%d rawPid=%d manual=%v", aggDetail.RawCid, aggDetail.RawPid, aggManual)
	}

	// 禁止直接选择合成一级分类 ID
	if err := srv.SaveFilmDetail(model.FilmDetailVo{Name: "测试片", Cid: catalog.SyntheticRootTypeOffset + 1, PlayLink: "https://example.com/a.m3u8"}); err == nil {
		t.Fatalf("expected error when saving film with synthetic category id")
	}

	// 非自建采集影片：编辑时即使提交空播放地址 + manualLinesUpdate=true，也禁止覆盖/清空采集站线路
	if err := gdb.Create(&model.MovieSourceMapping{SourceId: "src1", SourceMid: 9001, GlobalMid: 501}).Error; err != nil {
		t.Fatal(err)
	}
	if err := srv.SaveFilmDetail(model.FilmDetailVo{
		Id:                501,
		SourceId:          "src1",
		Name:              "聚合影片已更新",
		Pid:               1,
		Cid:               1,
		CName:             "电影",
		ManualLinesUpdate: true,
		PlayLink:          "",
	}); err != nil {
		t.Fatalf("save aggregated film failed: %v", err)
	}
	var lineCount int64
	gdb.Model(&model.FilmSourcePlaylist{}).Where("mid = ? AND source_id = ?", 501, "src1").Count(&lineCount)
	if lineCount != 1 {
		t.Fatalf("expected collected src1 playlist protected, got %d", lineCount)
	}

	// 自建影片（含负数 source_mid 映射）：允许更新为有效播放线路，但禁止编辑时清空全部播放地址
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 502, FirstSourceId: "src1"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1, Cid: 1, CName: "电影", CategoryKey: "source:src1:1", RootCategoryKey: "source:src1:1"},
		FilmIndexContent:  model.FilmIndexContent{Name: "自建影片"},
	})
	gdb.Create(&model.MovieSourceMapping{SourceId: "src1", SourceMid: -100502, GlobalMid: 502})
	gdb.Create(&model.FilmSourcePlaylist{
		Mid: 502, SourceId: "src1", LineKind: model.LineKindPlay, GroupIndex: 0, GroupName: "自建线路",
		Content: `[{"episode":"第1集","link":"https://example.com/manual.m3u8"}]`,
	})
	manualDetail, isManual, err := srv.GetStoredFilmDetail(502)
	if err != nil {
		t.Fatalf("get stored manual film detail failed: %v", err)
	}
	if !isManual || manualDetail.RawCid != 1 || manualDetail.RawPid != 1 {
		t.Fatalf("expected manual film RawCid=1 RawPid=1 manual=true, got rawCid=%d rawPid=%d manual=%v", manualDetail.RawCid, manualDetail.RawPid, isManual)
	}
	if err := srv.SaveFilmDetail(model.FilmDetailVo{
		Id:                502,
		SourceId:          "src1",
		Name:              "自建影片已更新",
		Pid:               1,
		Cid:               1,
		CName:             "电影",
		ManualLinesUpdate: true,
		PlayLink:          "",
	}); err == nil {
		t.Fatalf("expected error when clearing all play links on manual film")
	}
	gdb.Model(&model.FilmSourcePlaylist{}).Where("mid = ? AND source_id = ?", 502, "src1").Count(&lineCount)
	if lineCount != 1 {
		t.Fatalf("expected existing manual film playlist kept after rejected clear, got %d", lineCount)
	}
	if err := srv.SaveFilmDetail(model.FilmDetailVo{
		Id:                502,
		SourceId:          "src1",
		Name:              "自建影片已更新",
		Pid:               1,
		Cid:               1,
		CName:             "电影",
		ManualLinesUpdate: true,
		PlayLink:          "第1集$https://example.com/updated.m3u8",
	}); err != nil {
		t.Fatalf("save manual film with valid link failed: %v", err)
	}
	var updatedLine model.FilmSourcePlaylist
	if err := gdb.Where("mid = ? AND source_id = ?", 502, "src1").First(&updatedLine).Error; err != nil {
		t.Fatalf("reload manual playlist failed: %v", err)
	}
	if updatedLine.Content == `[{"episode":"第1集","link":"https://example.com/manual.m3u8"}]` || updatedLine.Content == "" {
		t.Fatalf("expected manual playlist updated to new link, got %s", updatedLine.Content)
	}
}
