package repository

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) *gorm.DB {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := gdb.AutoMigrate(&model.CollectRecord{}); err != nil {
		t.Fatalf("migrate collect record failed: %v", err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() {
		db.Mdb = prev
	})
	return gdb
}

func TestCollectRecordLifecycle(t *testing.T) {
	setupTestDB(t)

	now := time.Now()
	rec := &model.CollectRecord{
		BatchID:     "batch-test-1",
		Trigger:     model.CollectTriggerManual,
		Tag:         "单站采集",
		Hour:        24,
		Status:      model.CollectRecordStatusRunning,
		SourceCount: 1,
		StartedAt:   now,
	}

	if err := CreateCollectRecord(rec); err != nil {
		t.Fatalf("create collect record: %v", err)
	}

	// 验证更新结算
	comp := model.CollectRecordCompletion{
		Status:      model.CollectRecordStatusSuccess,
		FinishedAt:  now.Add(5 * time.Second),
		DurationMs:  5000,
		FilmCount:   10,
		PageTotal:   2,
		PageSuccess: 2,
		PageFailed:  0,
		ErrorMsg:    "",
		DetailJson:  `[{"sourceId":"s1","sourceName":"源1","status":"success"}]`,
	}
	if err := CompleteCollectRecord("batch-test-1", comp); err != nil {
		t.Fatalf("complete collect record: %v", err)
	}

	// 验证查询
	list, total, err := GetCollectRecordList(model.CollectRecordQuery{
		Page:     1,
		PageSize: 10,
		Trigger:  model.CollectTriggerManual,
	})
	if err != nil {
		t.Fatalf("list collect records: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("expected 1 record, got total=%d, len=%d", total, len(list))
	}
	if list[0].Status != model.CollectRecordStatusSuccess || list[0].FilmCount != 10 {
		t.Fatalf("unexpected record content: %+v", list[0])
	}

	// 验证按 ID 批量删除
	if err := DeleteCollectRecords([]uint{list[0].ID}); err != nil {
		t.Fatalf("delete record: %v", err)
	}
	_, totalAfterDel, err := GetCollectRecordList(model.CollectRecordQuery{})
	if err != nil || totalAfterDel != 0 {
		t.Fatalf("expected 0 after delete, got %d, err=%v", totalAfterDel, err)
	}
}

func TestCleanOldCollectRecords(t *testing.T) {
	setupTestDB(t)

	oldTime := time.Now().AddDate(0, 0, -35)
	newTime := time.Now().AddDate(0, 0, -5)

	_ = CreateCollectRecord(&model.CollectRecord{
		BatchID:   "batch-old",
		Trigger:   model.CollectTriggerCron,
		Status:    model.CollectRecordStatusSuccess,
		StartedAt: oldTime,
	})
	_ = CreateCollectRecord(&model.CollectRecord{
		BatchID:   "batch-new",
		Trigger:   model.CollectTriggerCron,
		Status:    model.CollectRecordStatusSuccess,
		StartedAt: newTime,
	})

	if err := CleanOldCollectRecords(30); err != nil {
		t.Fatalf("clean old records: %v", err)
	}

	list, total, err := GetCollectRecordList(model.CollectRecordQuery{})
	if err != nil || total != 1 || list[0].BatchID != "batch-new" {
		t.Fatalf("expected only batch-new, got total=%d, list=%+v", total, list)
	}
}

func TestMarkInterruptedCollectRecords(t *testing.T) {
	setupTestDB(t)

	_ = CreateCollectRecord(&model.CollectRecord{
		BatchID:   "batch-running",
		Trigger:   model.CollectTriggerManual,
		Status:    model.CollectRecordStatusRunning,
		StartedAt: time.Now(),
	})

	if err := MarkInterruptedCollectRecords(); err != nil {
		t.Fatalf("mark interrupted: %v", err)
	}

	list, _, err := GetCollectRecordList(model.CollectRecordQuery{})
	if err != nil || len(list) != 1 {
		t.Fatalf("query failed: %v", err)
	}
	if list[0].Status != model.CollectRecordStatusInterrupted {
		t.Fatalf("expected status interrupted, got %s", list[0].Status)
	}
}

func TestCompleteCollectRecordMissing(t *testing.T) {
	setupTestDB(t)

	err := CompleteCollectRecord("missing-batch", model.CollectRecordCompletion{
		Status:     model.CollectRecordStatusFailed,
		FinishedAt: time.Now(),
	})
	if err == nil || !strings.Contains(err.Error(), "采集记录不存在") {
		t.Fatalf("expected 采集记录不存在, got %v", err)
	}
}
