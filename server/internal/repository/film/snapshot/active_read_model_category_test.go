package snapshot

import (
	"strings"
	"testing"

	"server/internal/model"
	"server/internal/model/dto"

	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestApplyCategoryHotIndexHint_SQLiteUnchanged(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open("file:hint_sqlite?mode=memory&cache=shared"), &gorm.Config{
		DryRun: true,
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	sql := gdb.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []model.FilmIndex
		q := tx.Model(&model.FilmIndex{}).Select("mid").Where("pid = ?", 1)
		q = applyCategoryHotIndexHint(q, "pid")
		return q.Order("hits DESC, mid DESC").Limit(10).Find(&rows)
	})
	if strings.Contains(strings.ToUpper(sql), "USE INDEX") {
		t.Fatalf("sqlite must not inject USE INDEX, got: %s", sql)
	}
}

func TestApplyCategoryHotIndexHint_MySQLQuotedSeparately(t *testing.T) {
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

	assertHint := func(field, index string) {
		t.Helper()
		sql := gdb.ToSQL(func(tx *gorm.DB) *gorm.DB {
			var rows []model.FilmIndex
			q := tx.Model(&model.FilmIndex{}).Select("mid").Where("pid = ?", 1)
			q = applyCategoryHotIndexHint(q, field)
			return q.Order("hits DESC, mid DESC").Limit(10).Find(&rows)
		})
		if strings.Contains(sql, "`film_index USE INDEX") {
			t.Fatalf("table name must not swallow USE INDEX, got: %s", sql)
		}
		wantTable := "`film_index`"
		wantHint := "USE INDEX (`" + index + "`)"
		if !strings.Contains(sql, wantTable) || !strings.Contains(sql, wantHint) {
			t.Fatalf("expected quoted table + %s, got: %s", wantHint, sql)
		}
	}

	assertHint("pid", "idx_pid_hits")
	assertHint("cid", "idx_cid_hits")
}

func TestHotCategoryQuery_DrivesFromPlaylistIndex(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open("file:hot_member_sqlite?mode=memory&cache=shared"), &gorm.Config{
		DryRun: true,
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sql := gdb.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []model.FilmIndex
		return hotCategoryQuery(tx, "src_new", "pid", 29).Limit(14).Find(&rows)
	})
	if !strings.Contains(sql, "film_source_playlists") || !strings.Contains(sql, "hot_members") {
		t.Fatalf("hot query must start from source playlists, got: %s", sql)
	}
	if strings.Contains(strings.ToUpper(sql), "USE INDEX") {
		t.Fatalf("sqlite hot query must not inject USE INDEX, got: %s", sql)
	}
	if strings.Contains(sql, "idx_pid_hits") {
		t.Fatalf("source hot query must not scan idx_pid_hits, got: %s", sql)
	}

	mysqlDB, err := gorm.Open(mysql.New(mysql.Config{
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
	mysqlSQL := mysqlDB.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []model.FilmIndex
		return hotCategoryQuery(tx, "src_new", "pid", 29).Limit(14).Find(&rows)
	})
	if !strings.Contains(mysqlSQL, "USE INDEX (`idx_playlist_source_mid`)") {
		t.Fatalf("mysql hot query must use playlist source index, got: %s", mysqlSQL)
	}
	if strings.Contains(mysqlSQL, "idx_pid_hits") {
		t.Fatalf("mysql hot query must not scan idx_pid_hits, got: %s", mysqlSQL)
	}
}

func TestSortFastHits_DrivesFromPlaylistIndex(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open("file:sort_fast_hot_sqlite?mode=memory&cache=shared"), &gorm.Config{
		DryRun: true,
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sql := gdb.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []model.FilmIndex
		return categorySortFastQuery(tx, "src_new", 1, 29).Limit(21).Find(&rows)
	})
	if !strings.Contains(sql, "film_source_playlists") || !strings.Contains(sql, "hot_members") {
		t.Fatalf("classify hits top must start from source playlists, got: %s", sql)
	}
	if strings.Contains(sql, "idx_pid_hits") {
		t.Fatalf("classify hits top must not scan idx_pid_hits, got: %s", sql)
	}

	recentSQL := gdb.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []model.FilmIndex
		return categorySortFastQuery(tx, "src_new", 2, 29).Limit(21).Find(&rows)
	})
	if !strings.Contains(recentSQL, "film_source_playlists") || !strings.Contains(recentSQL, "root_category_key") {
		t.Fatalf("classify recent must filter by this source category key, got: %s", recentSQL)
	}
	if strings.Contains(recentSQL, "idx_pid_hits") || strings.Contains(recentSQL, "pid =") {
		t.Fatalf("classify recent must not match the merged local pid, got: %s", recentSQL)
	}

	mysqlDB, err := gorm.Open(mysql.New(mysql.Config{
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
	mysqlSQL := mysqlDB.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []model.FilmIndex
		return categorySortFastQuery(tx, "src_new", 1, 29).Order("hits DESC, mid DESC").Limit(21).Find(&rows)
	})
	if !strings.Contains(mysqlSQL, "USE INDEX (`idx_playlist_source_mid`)") {
		t.Fatalf("mysql classify hits top must use playlist source index, got: %s", mysqlSQL)
	}
	if strings.Contains(mysqlSQL, "idx_pid_hits") {
		t.Fatalf("mysql classify hits top must not scan idx_pid_hits, got: %s", mysqlSQL)
	}
}

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
	if err := gdb.AutoMigrate(&model.CategoryMapping{}); err != nil {
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

	got := GetSnapshotMovieListByCategoryWithSourceReadModel("vtest", "src_new", "pid", 2, 14, 0)
	if len(got) != 1 || got[0].Id != 501 {
		t.Fatalf("latest drama list = %+v, want mid 501", got)
	}
	hot := GetSnapshotHotMovieListByCategoryWithSourceReadModel("vtest", "src_new", "pid", 2, 14, 0)
	if len(hot) != 1 || hot[0].Id != 501 {
		t.Fatalf("hot drama list = %+v, want mid 501", hot)
	}
}

func TestProbeBranchSQL_MySQLUsesCategoryKeyIndex(t *testing.T) {
	branch := probeBranch{column: "root_category_key", value: "source:src:1", index: probeIndexRootHits}
	sql := probeBranchSQL("mysql", branch, "hits")
	if !strings.Contains(sql, "USE INDEX (`idx_root_key_hits_val`)") {
		t.Fatalf("mysql probe must walk the category hits index, got: %s", sql)
	}
	if !strings.Contains(sql, "EXISTS") || strings.Contains(sql, "hot_members") || strings.Contains(sql, "idx_playlist_source_mid") {
		t.Fatalf("probe must point-check playlist primary key, got: %s", sql)
	}
	sqliteSQL := probeBranchSQL("sqlite", branch, "update")
	if strings.Contains(strings.ToUpper(sqliteSQL), "USE INDEX") {
		t.Fatalf("sqlite probe must not inject USE INDEX, got: %s", sqliteSQL)
	}
}

func TestCategoryList_KeepsOtherSourceCategoryOut(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	films := []model.FilmIndex{
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, FirstSourceId: "src_a"},
			FilmIndexCategory: model.FilmIndexCategory{RootCategoryKey: "source:src_a:4", Pid: 20},
			FilmIndexContent:  model.FilmIndexContent{Name: "别站动漫", UpdateStamp: 10, Hits: 100},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, FirstSourceId: "src_new"},
			FilmIndexCategory: model.FilmIndexCategory{RootCategoryKey: "source:src_new:4"},
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

	got := GetSnapshotMovieListByCategoryWithSourceReadModel("vtest", "src_new", "pid", 4, 14, 0)
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
		Update("root_category_key", "source:source_master:1").Error; err != nil {
		t.Fatalf("set category key: %v", err)
	}
	got := GetSnapshotMovieListByCategoryWithSourceReadModel("vtest", "source_master", "pid", 1, 10, 0)
	if len(got) != 1 || got[0].Id != 11 {
		t.Fatalf("source list = %+v, want only mid 11", got)
	}
}

func TestCategoryList_LeafRootStoredOnCategoryKey(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
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

	got := GetSnapshotMovieListByCategoryWithSourceReadModel("vtest", "src_new", "pid", 21, 14, 0)
	if len(got) != 2 || got[0].Id != 21 || got[1].Id != 22 {
		t.Fatalf("pid 21 list = %+v, want mids 21 then 22", got)
	}
	movies := GetSnapshotMovieListByCategoryWithSourceReadModel("vtest", "src_new", "pid", 1, 14, 0)
	if len(movies) != 2 || movies[0].Id != 21 || movies[1].Id != 24 {
		t.Fatalf("pid 1 list = %+v, want mids 21 then 24", movies)
	}

	page := &dto.Page{Current: 1, PageSize: 20}
	searched := ListFilmSnapshotsByTagsReadModel("vtest", model.SearchTagsVO{Pid: 21, SourceId: "src_new"}, page)
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
	if !strings.Contains(sql, "root_category_key") || !strings.Contains(sql, "category_key") {
		t.Fatalf("pid match must accept either category key, got: %s", sql)
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
