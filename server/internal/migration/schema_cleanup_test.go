package migration

import (
	"testing"
	"time"

	"server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMigrateSchemaCleanup(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	// 模拟 v3.1.7 存量旧表：collect_record_films 为单列自增主键 + batch_id，home_recommend_group 含 builtin/builtin_key。
	if err := gdb.AutoMigrate(&model.CollectRecord{}, &model.HomeRecommendGroup{}); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`
		CREATE TABLE collect_record_films (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			batch_id TEXT NOT NULL,
			source_id TEXT NOT NULL,
			mid INTEGER NOT NULL,
			created_at DATETIME NOT NULL
		)
	`).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`ALTER TABLE home_recommend_group ADD COLUMN builtin INTEGER NOT NULL DEFAULT 0`).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`ALTER TABLE home_recommend_group ADD COLUMN builtin_key TEXT NOT NULL DEFAULT ''`).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`INSERT INTO home_recommend_group (id, source_id, title, enabled, builtin, builtin_key, created_at, updated_at) VALUES ('builtin-1', 's1', '每日更新', 1, 1, 'daily', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP), ('custom-1', 's1', '自定义专题', 1, 0, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`).Error; err != nil {
		t.Fatal(err)
	}

	rec := &model.CollectRecord{
		BatchID:   "legacy-batch",
		Trigger:   model.CollectTriggerManual,
		Status:    model.CollectRecordStatusSuccess,
		FilmCount: 10,
		Ledger:    true,
		StartedAt: time.Now(),
	}
	if err := gdb.Create(rec).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`INSERT INTO collect_record_films (batch_id, source_id, mid, created_at) VALUES ('legacy-batch', 's1', 101, CURRENT_TIMESTAMP)`).Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateSchemaCleanup(gdb); err != nil {
		t.Fatalf("migrateSchemaCleanup failed: %v", err)
	}
	if err := migrateSchemaCleanup(gdb); err != nil {
		t.Fatalf("second migrateSchemaCleanup failed: %v", err)
	}

	// 验证 collect_record_films 已重建为新聚簇结构，且可正常按 (record_id, source_id, mid) 写入。
	if gdb.Migrator().HasColumn(&model.CollectRecordFilmItem{}, "batch_id") ||
		!gdb.Migrator().HasColumn(&model.CollectRecordFilmItem{}, "record_id") {
		t.Fatal("collect_record_films was not rebuilt to new schema")
	}
	if err := gdb.Create(&model.CollectRecordFilmItem{RecordID: rec.ID, SourceID: "s1", Mid: 202}).Error; err != nil {
		t.Fatalf("insert into rebuilt collect_record_films failed: %v", err)
	}

	// 验证存量 collect_records 保留统计数但 ledger 已关闭。
	var updated model.CollectRecord
	if err := gdb.Where("id = ?", rec.ID).First(&updated).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Ledger || updated.FilmCount != 10 {
		t.Fatalf("record should keep filmCount=10 and disable ledger, got %+v", updated)
	}

	// 验证 home_recommend_group 废弃列及旧内置分组行已移除，自定义分组保留。
	for _, col := range []string{"builtin", "builtin_key"} {
		if gdb.Migrator().HasColumn(&model.HomeRecommendGroup{}, col) {
			t.Fatalf("legacy column %s should be dropped", col)
		}
	}
	var remainingIDs []string
	if err := gdb.Table(model.TableHomeRecommendGroup).Pluck("id", &remainingIDs).Error; err != nil {
		t.Fatal(err)
	}
	if len(remainingIDs) != 1 || remainingIDs[0] != "custom-1" {
		t.Fatalf("expected only custom-1 remaining after cleanup, got %v", remainingIDs)
	}
}
