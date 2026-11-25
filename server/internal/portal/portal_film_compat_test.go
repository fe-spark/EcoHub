package portal

import (
	"server/internal/film"
	"server/internal/film/catalog"
	"errors"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repobase"
	filmshared "server/internal/film/shared"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupFilmServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(
		&model.FilmSource{}, &model.FilmIndex{}, &model.FilmSourcePlaylist{},
		&model.Category{}, &model.SourceCategory{}, &model.CategoryMapping{},
		&model.MovieSourceMapping{}, &model.SiteConfigRecord{}, &model.PrimaryState{},
	); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	origMdb, origRdb := db.Mdb, db.Rdb
	db.Mdb, db.Rdb = gdb, nil
	filmsnapshot.ClearActiveFilmReadModel()
	filmsnapshot.ClearActiveSnapshotVersion()
	t.Cleanup(func() {
		db.Mdb, db.Rdb = origMdb, origRdb
		filmsnapshot.ClearActiveFilmReadModel()
		filmsnapshot.ClearActiveSnapshotVersion()
	})
	return gdb
}

func TestGetFilmDetail_ExcludesHiddenNonPrimaryCategoryPeerPlay(t *testing.T) {
	gdb, _ := setupTestDBAndRedis(t)
	t.Cleanup(func() { repobase.SetCategoryShowForTest(map[int64]bool{}) })

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
	catalog.MarkCategoryChanged()

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
	if err := new(film.FilmService).SaveSourceClassTree("src_b", []*model.CategoryTree{
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
	batch, _, err := film.BatchGetPlayPlaylistsByMids([]int64{101})
	if err != nil {
		t.Fatalf("BatchGetPlayPlaylistsByMids: %v", err)
	}
	if len(batch[101]) != 1 || batch[101][0].SourceId != "src_a" {
		t.Fatalf("expected TVBox batch to exclude hidden peer source, got %+v", batch[101])
	}

	// 4. 恢复二级显示但隐藏一级父分类「伦理」，同样不得出现
	if err := new(film.FilmService).SaveSourceClassTree("src_b", []*model.CategoryTree{
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

func TestGetNavCategory_RejectsUnknownOrDisabledSource(t *testing.T) {
	gdb, _ := setupTestDBAndRedis(t)
	sources := []model.FilmSource{
		{Id: "src1", Name: "站点一", Uri: "http://src1.test", State: true, Sort: 1},
		{Id: "src2", Name: "站点二", Uri: "http://src2.test", State: true, Sort: 2},
		{Id: "src3", Name: "停用站", Uri: "http://src3.test", State: false, Sort: 3},
	}
	if err := gdb.Create(&sources).Error; err != nil {
		t.Fatalf("seed sources: %v", err)
	}
	if _, err := IndexSvc.GetNavCategory("nope"); !errors.Is(err, ErrNavCategorySourceUnavailable) {
		t.Fatalf("unknown source should be rejected, got %v", err)
	}
	if _, err := IndexSvc.GetNavCategory("src3"); !errors.Is(err, ErrNavCategorySourceUnavailable) {
		t.Fatalf("disabled source should be rejected, got %v", err)
	}
	if _, err := IndexSvc.GetNavCategory("src1"); err != nil {
		t.Fatalf("enabled source should pass, got %v", err)
	}
}
