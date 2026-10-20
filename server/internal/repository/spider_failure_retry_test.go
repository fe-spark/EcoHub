package repository

import (
	"fmt"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMarkFailureRecordRetryFailed_FinalOnlyAtMax(t *testing.T) {
	prev := db.Mdb
	gdb, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.FailureRecord{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	row := model.FailureRecord{
		OriginId:   "src",
		OriginName: "站",
		PageNumber: 1,
		Status:     model.FailureRecordStatusPending,
		RetryCount: 1,
	}
	if err := gdb.Create(&row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	final, count, err := MarkFailureRecordRetryFailed(&row, "recover_fetch: empty", model.MaxFailureRetryCount)
	if err != nil {
		t.Fatal(err)
	}
	if final || count != 2 {
		t.Fatalf("第 2 次不应最终失败, final=%v count=%d", final, count)
	}

	var saved model.FailureRecord
	if err := gdb.First(&saved, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.RetryCount != 2 || saved.Status != model.FailureRecordStatusPending {
		t.Fatalf("库内状态 = count:%d status:%d", saved.RetryCount, saved.Status)
	}

	saved.RetryCount = model.MaxFailureRetryCount - 1
	if err := gdb.Model(&saved).Update("retry_count", saved.RetryCount).Error; err != nil {
		t.Fatal(err)
	}
	final, count, err = MarkFailureRecordRetryFailed(&saved, "recover_fetch: empty", model.MaxFailureRetryCount)
	if err != nil {
		t.Fatal(err)
	}
	if !final || count != model.MaxFailureRetryCount {
		t.Fatalf("满 %d 次应最终失败, final=%v count=%d", model.MaxFailureRetryCount, final, count)
	}
}
