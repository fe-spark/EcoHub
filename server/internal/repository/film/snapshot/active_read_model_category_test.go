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

	assertHint("pid", "idx_snap_pid_hits")
	assertHint("cid", "idx_snap_cid_hits")
}
