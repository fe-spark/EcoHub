package snapshot

import (
	"strings"
	"testing"

	"server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestApplyCategorySearchFilter_SourceScoped(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open("file:manage_search_sqlite?mode=memory&cache=shared"), &gorm.Config{
		DryRun: true,
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	// 1. 指定采集源与 pid (如源站韩国伦理 pid=57)
	sqlPid := gdb.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []model.FilmIndex
		q := tx.Model(&model.FilmIndex{})
		q = applyCategorySearchFilter(q, "1208629981", 57, 0)
		return q.Find(&rows)
	})

	if !strings.Contains(sqlPid, "root_category_key") || !strings.Contains(sqlPid, "category_key") {
		t.Fatalf("source-scoped pid search must match source category keys, got: %s", sqlPid)
	}
	if strings.Contains(sqlPid, "pid =") {
		t.Fatalf("source-scoped pid search must not match local pid column, got: %s", sqlPid)
	}
	if !strings.Contains(sqlPid, "1208629981:57") {
		t.Fatalf("source-scoped pid search must include formatted source key 1208629981:57, got: %s", sqlPid)
	}

	// 2. 指定采集源与 cid
	sqlCid := gdb.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []model.FilmIndex
		q := tx.Model(&model.FilmIndex{})
		q = applyCategorySearchFilter(q, "1208629981", 0, 58)
		return q.Find(&rows)
	})

	if !strings.Contains(sqlCid, "category_key") {
		t.Fatalf("source-scoped cid search must match category_key, got: %s", sqlCid)
	}
	if strings.Contains(sqlCid, "cid =") {
		t.Fatalf("source-scoped cid search must not match local cid column, got: %s", sqlCid)
	}
	if !strings.Contains(sqlCid, "1208629981:58") {
		t.Fatalf("source-scoped cid search must include formatted source key 1208629981:58, got: %s", sqlCid)
	}

	// 3. 无采集源 (全局分类检索，回退到管理类目兼容过滤)
	sqlGlobal := gdb.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []model.FilmIndex
		q := tx.Model(&model.FilmIndex{})
		q = applyCategorySearchFilter(q, "", 1, 0)
		return q.Find(&rows)
	})
	if sqlGlobal == "" {
		t.Fatalf("global category filter produced empty sql")
	}
}
