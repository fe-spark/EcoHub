package snapshot

import (
	"strings"
	"testing"

	"server/internal/model"

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
	if !strings.Contains(recentSQL, "EXISTS") || strings.Contains(recentSQL, "hot_members") {
		t.Fatalf("classify recent must keep playlist primary-key probe, got: %s", recentSQL)
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

	got := GetSnapshotMovieListByCategoryWithSourceReadModel("vtest", "src_new", "pid", 90, 14, 0)
	if len(got) != 1 || got[0].Id != 501 {
		t.Fatalf("latest drama list = %+v, want mid 501", got)
	}
	hot := GetSnapshotHotMovieListByCategoryWithSourceReadModel("vtest", "src_new", "pid", 90, 14, 0)
	if len(hot) != 1 || hot[0].Id != 501 {
		t.Fatalf("hot drama list = %+v, want mid 501", hot)
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

	got := GetSnapshotMovieListByCategoryWithSourceReadModel("vtest", "source_master", "pid", 1, 10, 0)
	if len(got) != 1 || got[0].Id != 11 {
		t.Fatalf("source list = %+v, want only mid 11", got)
	}
}
