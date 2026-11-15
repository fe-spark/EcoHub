package migration

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"server/internal/model"
)

func TestMigrateCollectRecordResetDropsOldRows(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.CollectRecord{}, &model.CollectRecordFilmItem{}); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.CollectRecord{
		BatchID:   "old-batch",
		Trigger:   "manual",
		Status:    model.CollectRecordStatusSuccess,
		Ledger:    true,
		StartedAt: time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateCollectRecordReset(gdb); err != nil {
		t.Fatal(err)
	}
	if err := migrateCollectRecordReset(gdb); err != nil {
		t.Fatal(err)
	}

	var n int64
	if err := gdb.Model(&model.CollectRecord{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("old records kept: %d", n)
	}
	var columns []struct {
		Name string `gorm:"column:name"`
	}
	if err := gdb.Raw("PRAGMA table_info(collect_records)").Scan(&columns).Error; err != nil {
		t.Fatal(err)
	}
	for _, col := range columns {
		if col.Name == "film_ids" {
			t.Fatal("film_ids column should not exist")
		}
	}

	fresh := model.CollectRecord{
		BatchID:   "new-batch",
		Trigger:   "manual",
		Status:    model.CollectRecordStatusRunning,
		StartedAt: time.Now(),
	}
	if err := gdb.Create(&fresh).Error; err != nil {
		t.Fatal(err)
	}
	var created model.CollectRecord
	if err := gdb.Where("batch_id = ?", "new-batch").First(&created).Error; err != nil {
		t.Fatal(err)
	}
	if created.Ledger {
		t.Fatal("new running record should not have a film ledger")
	}
}
