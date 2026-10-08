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
		var rows []model.FilmListSnapshot
		q := tx.Model(&model.FilmListSnapshot{}).Unscoped().Select("id").Where("snapshot_version = ?", "v1")
		q = applyCategoryHotIndexHint(q, "pid")
		return q.Order("hits DESC, id DESC").Limit(10).Find(&rows)
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
			var rows []model.FilmListSnapshot
			q := tx.Model(&model.FilmListSnapshot{}).Unscoped().Select("id").Where("snapshot_version = ?", "v1")
			q = applyCategoryHotIndexHint(q, field)
			return q.Order("hits DESC, id DESC").Limit(10).Find(&rows)
		})
		if strings.Contains(sql, "`film_list_snapshot USE INDEX") {
			t.Fatalf("table name must not swallow USE INDEX, got: %s", sql)
		}
		wantTable := "`film_list_snapshot`"
		wantHint := "USE INDEX (`" + index + "`)"
		if !strings.Contains(sql, wantTable) || !strings.Contains(sql, wantHint) {
			t.Fatalf("expected quoted table + %s, got: %s", wantHint, sql)
		}
	}

	assertHint("pid", "idx_pid_hits")
	assertHint("cid", "idx_cid_hits")
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
