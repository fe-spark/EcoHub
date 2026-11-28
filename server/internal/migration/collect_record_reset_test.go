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
	if err := gdb.AutoMigrate(&model.CollectRecord{}); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.CollectRecord{
		BatchID:   "old-batch",
		Trigger:   "manual",
		Status:    model.CollectRecordStatusSuccess,
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
}
