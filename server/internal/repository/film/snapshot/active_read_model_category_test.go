package snapshot

import (
	"strings"
	"testing"

	"server/internal/model"
	"server/internal/model/dto"

	"gorm.io/driver/mysql"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestApplyCategoryUpdateIndexHint_MySQLQuotedSeparately(t *testing.T) {
	gdb, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(127.0.0.1:3306)/gorm?charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
		Logger:               logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open mysql dry-run: %v", err)
	}

	sql := gdb.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []model.FilmIndex
		q := tx.Model(&model.FilmIndex{}).Select("mid").Where("pid = ?", 1)
		q = applyCategoryUpdateIndexHint(q, "pid")
		return q.Order("update_stamp DESC, mid DESC").Limit(14).Find(&rows)
	})
	if !strings.Contains(sql, "USE INDEX (`idx_film_index_pid_update_mid`)") {
		t.Fatalf("expected update index hint, got: %s", sql)
	}
	if strings.Contains(sql, "`film_index USE INDEX") {
		t.Fatalf("table name must not swallow USE INDEX, got: %s", sql)
	}
}

func TestCategoryList_ZeroPidUsesCategoryKey(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	if err := gdb.AutoMigrate(&model.CategoryMapping{}, &model.SourceCategory{}); err != nil {
		t.Fatalf("migrate mapping: %v", err)
	}
	categories := []model.Category{
		{Id: 90, Pid: 0, Name: "连续剧", Show: true, StableKey: "display:root:连续剧"},
		{Id: 91, Pid: 90, Name: "国产剧", Show: true, StableKey: "display:root:连续剧/国产剧"},
		{Id: 9, Pid: 0, Name: "电影", Show: true, StableKey: "display:root:电影"},
	}
	if err := gdb.Create(&categories).Error; err != nil {
		t.Fatalf("create categories: %v", err)
	}
	mappings := []model.CategoryMapping{
		{SourceId: "src_new", SourceTypeId: 2, CategoryId: 90},
		{SourceId: "src_new", SourceTypeId: 13, CategoryId: 91},
		{SourceId: "src_new", SourceTypeId: 1, CategoryId: 9},
	}
	if err := gdb.Create(&mappings).Error; err != nil {
		t.Fatalf("create mappings: %v", err)
	}
	sourceCategories := []model.SourceCategory{
		{SourceId: "src_new", SourceTypeId: 2, ParentSourceTypeId: 0, RawName: "连续剧", Sort: 1, Depth: 0},
		{SourceId: "src_new", SourceTypeId: 13, ParentSourceTypeId: 2, RawName: "国产剧", Sort: 1, Depth: 1},
		{SourceId: "src_new", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 2, Depth: 0},
	}
	if err := gdb.Create(&sourceCategories).Error; err != nil {
		t.Fatalf("create source categories: %v", err)
	}
	films := []model.FilmIndex{
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 501, FirstSourceId: "src_new"},
			FilmIndexCategory: model.FilmIndexCategory{RootCategoryKey: "source:src_new:2", CategoryKey: "source:src_new:13"},
			FilmIndexContent:  model.FilmIndexContent{Name: "国产剧片子", UpdateStamp: 100, Hits: 10},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 502, FirstSourceId: "src_new"},
			FilmIndexCategory: model.FilmIndexCategory{RootCategoryKey: "source:src_new:1", CategoryKey: "source:src_new:1"},
			FilmIndexContent:  model.FilmIndexContent{Name: "电影片子", UpdateStamp: 200, Hits: 20},
		},
	}
	if err := gdb.Create(&films).Error; err != nil {
		t.Fatalf("create films: %v", err)
	}
	for _, mid := range []int64{501, 502} {
		line := model.FilmSourcePlaylist{Mid: mid, SourceId: "src_new", LineKind: "play", GroupIndex: 0, GroupName: "线路"}
		if err := gdb.Create(&line).Error; err != nil {
			t.Fatalf("create playlist: %v", err)
		}
	}
	if err := SetActiveSnapshotVersion("vtest"); err != nil {
		t.Fatalf("set version: %v", err)
	}

	got, err := GetSnapshotMovieListByCategoryWithSourceReadModel("vtest", "src_new", "pid", 2, 14, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Id != 501 {
		t.Fatalf("latest drama list = %+v, want mid 501", got)
	}
	hot, err := GetSnapshotHotMovieListByCategoryWithSourceReadModel("vtest", "src_new", "pid", 2, 14, 0)
	if err != nil {
		t.Fatalf("hot list: %v", err)
	}
	if len(hot) != 1 || hot[0].Id != 501 {
		t.Fatalf("hot drama list = %+v, want mid 501", hot)
	}
}

func TestCategoryRank_ThreeSortsShareBoard(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	films := []model.FilmIndex{
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, FirstSourceId: "src"},
			FilmIndexCategory: model.FilmIndexCategory{CategoryKey: "source:src:1", Pid: 1},
			FilmIndexContent:  model.FilmIndexContent{Name: "旧片", Year: 2001, Hits: 10, UpdateStamp: 100},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, FirstSourceId: "src"},
			FilmIndexCategory: model.FilmIndexCategory{CategoryKey: "source:src:1", Pid: 1},
			FilmIndexContent:  model.FilmIndexContent{Name: "热片", Year: 2010, Hits: 900, UpdateStamp: 300},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 3, FirstSourceId: "src"},
			FilmIndexCategory: model.FilmIndexCategory{CategoryKey: "source:src:1", Pid: 1},
			FilmIndexContent:  model.FilmIndexContent{Name: "新年", Year: 2024, Hits: 50, UpdateStamp: 200},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 6, FirstSourceId: "src"},
			FilmIndexCategory: model.FilmIndexCategory{CategoryKey: "source:src:1", Pid: 1},
			FilmIndexContent:  model.FilmIndexContent{Name: "同年更旧", Year: 2024, Hits: 1, UpdateStamp: 50},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 4, FirstSourceId: "src"},
			FilmIndexCategory: model.FilmIndexCategory{CategoryKey: "source:src:1", Pid: 1},
			FilmIndexContent:  model.FilmIndexContent{Name: "无播放", Year: 2099, Hits: 9999, UpdateStamp: 9999},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 5, FirstSourceId: "other"},
			FilmIndexCategory: model.FilmIndexCategory{CategoryKey: "source:other:1", Pid: 1},
			FilmIndexContent:  model.FilmIndexContent{Name: "别站", Year: 2030, Hits: 8000, UpdateStamp: 8000},
		},
	}
	if err := gdb.Create(&films).Error; err != nil {
		t.Fatalf("create films: %v", err)
	}
	for _, mid := range []int64{1, 2, 3, 6} {
		line := model.FilmSourcePlaylist{Mid: mid, SourceId: "src", LineKind: "play", GroupIndex: 0}
		if err := gdb.Create(&line).Error; err != nil {
			t.Fatalf("create playlist: %v", err)
		}
	}
	other := model.FilmSourcePlaylist{Mid: 5, SourceId: "other", LineKind: "play", GroupIndex: 0}
	if err := gdb.Create(&other).Error; err != nil {
		t.Fatalf("create other playlist: %v", err)
	}
	if err := SetActiveSnapshotVersion("vrank"); err != nil {
		t.Fatalf("set version: %v", err)
	}
	if err := LoadActiveFilmReadModel("vrank"); err != nil {
		t.Fatalf("load read model: %v", err)
	}
	WaitActiveFilmSearchIndexBuilt()

	news, err := GetSnapshotTopMoviesBySortFastWithSource("vrank", "src", 0, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if ids := movieIDs(news); !sameIDs(ids, []int64{3, 6, 2, 1}) {
		t.Fatalf("year order = %v", ids)
	}
	top, err := GetSnapshotTopMoviesBySortFastWithSource("vrank", "src", 1, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if ids := movieIDs(top); !sameIDs(ids, []int64{2, 3, 1, 6}) {
		t.Fatalf("hits order = %v", ids)
	}
	recent, err := GetSnapshotTopMoviesBySortFastWithSource("vrank", "src", 2, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if ids := movieIDs(recent); !sameIDs(ids, []int64{2, 3, 1, 6}) {
		t.Fatalf("update order = %v", ids)
	}
	hot, err := GetSnapshotHotMovieListByCategoryWithSourceReadModel("vrank", "src", "pid", 1, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ids := movieIDs(hot); !sameIDs(ids, movieIDs(top)) {
		t.Fatalf("hot list = %v, classify top = %v", ids, movieIDs(top))
	}
}

func movieIDs(list []model.MovieBasicInfo) []int64 {
	ids := make([]int64, len(list))
	for i := range list {
		ids[i] = list[i].Id
	}
	return ids
}

func sameIDs(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestProbeBranchSQL_MySQLUsesCategoryKeyIndex(t *testing.T) {
	branch := probeBranch{column: "root_category_key", value: "source:src:1", index: probeIndexRootUpdate}
	sql := probeBranchSQL("mysql", branch)
	if !strings.Contains(sql, "USE INDEX (`idx_root_key_upd_stamp`)") {
		t.Fatalf("mysql probe must walk the category update index, got: %s", sql)
	}
	if !strings.Contains(sql, "EXISTS") || strings.Contains(sql, "hot_members") || strings.Contains(sql, "idx_playlist_source_mid") {
		t.Fatalf("probe must point-check playlist primary key, got: %s", sql)
	}
	sqliteSQL := probeBranchSQL("sqlite", branch)
	if strings.Contains(strings.ToUpper(sqliteSQL), "USE INDEX") {
		t.Fatalf("sqlite probe must not inject USE INDEX, got: %s", sqliteSQL)
	}
}

func TestCategoryList_KeepsOtherSourceCategoryOut(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	films := []model.FilmIndex{
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, FirstSourceId: "src_a"},
			FilmIndexCategory: model.FilmIndexCategory{RootCategoryKey: "source:src_a:4", CategoryKey: "source:src_a:4", Pid: 20},
			FilmIndexContent:  model.FilmIndexContent{Name: "别站动漫", UpdateStamp: 10, Hits: 100},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, FirstSourceId: "src_new"},
			FilmIndexCategory: model.FilmIndexCategory{RootCategoryKey: "source:src_new:4", CategoryKey: "source:src_new:4"},
			FilmIndexContent:  model.FilmIndexContent{Name: "本站动漫", UpdateStamp: 50, Hits: 1},
		},
	}
	if err := gdb.Create(&films).Error; err != nil {
		t.Fatalf("create films: %v", err)
	}
	for _, mid := range []int64{1, 2} {
		line := model.FilmSourcePlaylist{Mid: mid, SourceId: "src_new", LineKind: "play", GroupIndex: 0}
		if err := gdb.Create(&line).Error; err != nil {
			t.Fatalf("create playlist: %v", err)
		}
	}
	if err := SetActiveSnapshotVersion("vtest"); err != nil {
		t.Fatalf("set version: %v", err)
	}

	got, err := GetSnapshotMovieListByCategoryWithSourceReadModel("vtest", "src_new", "pid", 4, 14, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Id != 2 {
		t.Fatalf("source type list = %+v, want only mid 2", got)
	}
}

func TestSourceMembership_UsesPrimaryKeyProbe(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	createTestFilm(t, gdb, 11, "有线路", 100, "")
	indexOnly := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 12, FirstSourceId: "other"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1, Cid: 10, CName: "动作片"},
		FilmIndexContent:  model.FilmIndexContent{Name: "无线路", UpdateStamp: 200},
	}
	if err := gdb.Create(&indexOnly).Error; err != nil {
		t.Fatalf("create film without playlist: %v", err)
	}

	sql := gdb.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []model.FilmIndex
		return applySourceMembership(tx.Model(&model.FilmIndex{}), "source_master").Find(&rows)
	})
	if !strings.Contains(sql, "EXISTS") || strings.Contains(strings.ToUpper(sql), "DISTINCT") {
		t.Fatalf("source filter must probe playlist primary key, got: %s", sql)
	}

	if err := gdb.Model(&model.FilmIndex{}).Where("mid IN ?", []int64{11, 12}).
		Update("category_key", "source:source_master:1").Error; err != nil {
		t.Fatalf("set category key: %v", err)
	}
	got, err := GetSnapshotMovieListByCategoryWithSourceReadModel("vtest", "source_master", "pid", 1, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Id != 11 {
		t.Fatalf("source list = %+v, want only mid 11", got)
	}
}

func TestCategoryList_LeafRootStoredOnCategoryKey(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	if err := gdb.AutoMigrate(&model.SourceCategory{}); err != nil {
		t.Fatalf("migrate source categories: %v", err)
	}
	sourceCategories := []model.SourceCategory{
		{SourceId: "src_new", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 1, Depth: 0},
		{SourceId: "src_new", SourceTypeId: 13, ParentSourceTypeId: 1, RawName: "动作片", Sort: 1, Depth: 1},
		{SourceId: "src_new", SourceTypeId: 21, ParentSourceTypeId: 0, RawName: "纪录片", Sort: 2, Depth: 0},
	}
	if err := gdb.Create(&sourceCategories).Error; err != nil {
		t.Fatalf("create source categories: %v", err)
	}
	films := []model.FilmIndex{
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 21, FirstSourceId: "src_new"},
			FilmIndexCategory: model.FilmIndexCategory{RootCategoryKey: "source:src_new:1", CategoryKey: "source:src_new:21"},
			FilmIndexContent:  model.FilmIndexContent{Name: "纪录片叶子", UpdateStamp: 300, Hits: 8},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 22, FirstSourceId: "src_new"},
			FilmIndexCategory: model.FilmIndexCategory{RootCategoryKey: "source:src_new:21", CategoryKey: "source:src_new:21"},
			FilmIndexContent:  model.FilmIndexContent{Name: "纪录片根", UpdateStamp: 200, Hits: 4},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 23, FirstSourceId: "src_other"},
			FilmIndexCategory: model.FilmIndexCategory{RootCategoryKey: "source:src_other:1", CategoryKey: "source:src_other:21"},
			FilmIndexContent:  model.FilmIndexContent{Name: "别站纪录片", UpdateStamp: 400, Hits: 99},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 24, FirstSourceId: "src_new"},
			FilmIndexCategory: model.FilmIndexCategory{RootCategoryKey: "source:src_new:1", CategoryKey: "source:src_new:13"},
			FilmIndexContent:  model.FilmIndexContent{Name: "电影动作", UpdateStamp: 100, Hits: 1},
		},
	}
	if err := gdb.Create(&films).Error; err != nil {
		t.Fatalf("create films: %v", err)
	}
	lines := []model.FilmSourcePlaylist{
		{Mid: 21, SourceId: "src_new", LineKind: "play", GroupIndex: 0},
		{Mid: 22, SourceId: "src_new", LineKind: "play", GroupIndex: 0},
		{Mid: 23, SourceId: "src_other", LineKind: "play", GroupIndex: 0},
		{Mid: 24, SourceId: "src_new", LineKind: "play", GroupIndex: 0},
	}
	if err := gdb.Create(&lines).Error; err != nil {
		t.Fatalf("create playlists: %v", err)
	}
	if err := SetActiveSnapshotVersion("vtest"); err != nil {
		t.Fatalf("set version: %v", err)
	}

	got, err := GetSnapshotMovieListByCategoryWithSourceReadModel("vtest", "src_new", "pid", 21, 14, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Id != 21 || got[1].Id != 22 {
		t.Fatalf("pid 21 list = %+v, want mids 21 then 22", got)
	}
	movies, err := GetSnapshotMovieListByCategoryWithSourceReadModel("vtest", "src_new", "pid", 1, 14, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(movies) != 1 || movies[0].Id != 24 {
		t.Fatalf("pid 1 list = %+v, want only mid 24 (must not include independent leaf root 21)", movies)
	}

	page := &dto.Page{Current: 1, PageSize: 20}
	searched := mustListTags(t, "vtest", model.SearchTagsVO{Pid: 21, SourceId: "src_new"}, page)
	if page.Total != 2 || len(searched) != 2 || searched[0].Mid != 21 || searched[1].Mid != 22 {
		t.Fatalf("library pid 21 = total %d %+v, want mids 21 then 22", page.Total, searched)
	}
}

func TestSourceTypePidMatch_IncludesCategoryKey(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open("file:source_type_pid?mode=memory&cache=shared"), &gorm.Config{
		DryRun: true,
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sql := gdb.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []model.FilmIndex
		return categoryFilmQuery(tx, "src_new", "pid", 21, basicSelectFields).Find(&rows)
	})
	if strings.Contains(sql, "root_category_key") || !strings.Contains(sql, "category_key") {
		t.Fatalf("pid match must filter by category_key without root_category_key pollution, got: %s", sql)
	}
	if strings.Contains(sql, "pid =") {
		t.Fatalf("pid match must not use the local pid column, got: %s", sql)
	}
	cidSQL := gdb.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []model.FilmIndex
		return categoryFilmQuery(tx, "src_new", "cid", 21, basicSelectFields).Find(&rows)
	})
	if strings.Contains(cidSQL, "root_category_key") || !strings.Contains(cidSQL, "category_key") {
		t.Fatalf("cid match must stay on category_key, got: %s", cidSQL)
	}
}
