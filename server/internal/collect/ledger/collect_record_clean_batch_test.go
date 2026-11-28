package ledger

import (
	"fmt"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCleanOldCollectRecords_BatchedDeletionPreservesRunning(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	origDB := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = origDB })

	if err := gdb.AutoMigrate(&model.CollectRecord{}); err != nil {
		t.Fatal(err)
	}

	origChunk := collectCleanRecordChunk
	collectCleanRecordChunk = 2
	t.Cleanup(func() { collectCleanRecordChunk = origChunk })

	oldTime := time.Now().AddDate(0, 0, -40)
	recentTime := time.Now().AddDate(0, 0, -5)

	for i := 0; i < 5; i++ {
		if err := gdb.Create(&model.CollectRecord{
			BatchID:   fmt.Sprintf("old_done_%d", i),
			Trigger:   model.CollectTriggerCron,
			Status:    model.CollectRecordStatusSuccess,
			FilmCount: 10,
			StartedAt: oldTime,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := gdb.Create(&model.CollectRecord{
		BatchID:   "old_running",
		Trigger:   model.CollectTriggerManual,
		Status:    model.CollectRecordStatusRunning,
		StartedAt: oldTime,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.CollectRecord{
		BatchID:   "recent_done",
		Trigger:   model.CollectTriggerManual,
		Status:    model.CollectRecordStatusSuccess,
		FilmCount: 5,
		StartedAt: recentTime,
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := CleanOldCollectRecords(30); err != nil {
		t.Fatalf("CleanOldCollectRecords failed: %v", err)
	}

	var remaining []string
	if err := gdb.Model(&model.CollectRecord{}).Order("batch_id ASC").Pluck("batch_id", &remaining).Error; err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 2 || remaining[0] != "old_running" || remaining[1] != "recent_done" {
		t.Fatalf("unexpected remaining records: %v", remaining)
	}
}
