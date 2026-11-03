package migration

import (
	"testing"

	"server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRunAutoMigrations_EmptyBaselineIdempotent(t *testing.T) {
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test sqlite: %v", err)
	}
	if err := testDB.AutoMigrate(&model.SchemaMigration{}); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	if err := RunAutoMigrations(testDB); err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	var count int64
	if err := testDB.Model(&model.SchemaMigration{}).Count(&count).Error; err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != int64(len(migrations)) {
		t.Fatalf("expected %d migrations, got %d", len(migrations), count)
	}

	var row model.SchemaMigration
	if err := testDB.First(&row).Error; err != nil {
		t.Fatalf("load baseline: %v", err)
	}
	if row.Version != "20261009_baseline" {
		t.Fatalf("unexpected version %s", row.Version)
	}

	if err := RunAutoMigrations(testDB); err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	var countAfter int64
	if err := testDB.Model(&model.SchemaMigration{}).Count(&countAfter).Error; err != nil {
		t.Fatalf("count after second run: %v", err)
	}
	if countAfter != int64(len(migrations)) {
		t.Fatalf("expected count to stay %d, got %d", len(migrations), countAfter)
	}

	var clean model.CrontabRecord
	if err := testDB.Where("task_id = ?", model.TaskIDCollectClean).First(&clean).Error; err != nil {
		t.Fatalf("default collect clean: %v", err)
	}
	if !clean.State || clean.Spec != "0 30 3 * * *" || clean.TaskModel != model.TaskModelCollectClean {
		t.Fatalf("default collect clean = %+v", clean)
	}
}

func TestRunAutoMigrations_NilDB(t *testing.T) {
	if err := RunAutoMigrations(nil); err != nil {
		t.Fatalf("nil db should be a no-op, got %v", err)
	}
}

func TestRunAutoMigrations_CollectRecordsRefactor(t *testing.T) {
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	// 模拟旧版本数据库环境：有旧表 failure_records 和旧任务
	_ = testDB.AutoMigrate(&model.CrontabRecord{})
	_ = testDB.Table(model.TableFailureRecord).Exec("CREATE TABLE " + model.TableFailureRecord + " (id integer primary key)").Error
	_ = testDB.Create(&model.CrontabRecord{TaskId: "sys_cron_recover_collect", TaskModel: 1, Spec: "0 0 2 * * *"}).Error
	_ = testDB.Create(&model.CrontabRecord{TaskId: "user_custom_task", TaskModel: 1, Spec: "0 12 * * *"}).Error
	_ = testDB.Create(&model.CrontabRecord{
		TaskId:    model.TaskIDFailureClean,
		TaskModel: 4,
		Spec:      "0 15 4 * * *",
		State:     false,
		Remark:    "old",
	}).Error
	// 执行版本迁移
	if err := RunAutoMigrations(testDB); err != nil {
		t.Fatalf("RunAutoMigrations failed: %v", err)
	}

	// 验证旧任务已删除
	var countRecover int64
	_ = testDB.Model(&model.CrontabRecord{}).Where("task_id = ?", "sys_cron_recover_collect").Count(&countRecover)
	if countRecover != 0 {
		t.Fatalf("expected sys_cron_recover_collect to be deleted, got %d", countRecover)
	}
	var countFailureClean int64
	_ = testDB.Model(&model.CrontabRecord{}).Where("task_id = ?", model.TaskIDFailureClean).Count(&countFailureClean)
	if countFailureClean != 0 {
		t.Fatalf("expected sys_cron_failure_clean to be deleted, got %d", countFailureClean)
	}
	var countCustom int64
	_ = testDB.Model(&model.CrontabRecord{}).Where("task_id = ?", "user_custom_task").Count(&countCustom)
	if countCustom != 1 {
		t.Fatalf("user_custom_task should not be deleted, got count %d", countCustom)
	}

	// 验证新任务已创建
	var newCleanTask model.CrontabRecord
	if err := testDB.Where("task_id = ?", model.TaskIDCollectClean).First(&newCleanTask).Error; err != nil {
		t.Fatalf("expected sys_cron_collect_clean to exist: %v", err)
	}
	if newCleanTask.TaskModel != model.TaskModelCollectClean {
		t.Fatalf("expected model %d, got %d", model.TaskModelCollectClean, newCleanTask.TaskModel)
	}
	if newCleanTask.State {
		t.Fatal("disabled cleanup job was turned back on")
	}
	if newCleanTask.Spec != "0 15 4 * * *" {
		t.Fatalf("spec changed: %s", newCleanTask.Spec)
	}
	if newCleanTask.Remark != "定期清理超过 30 天的采集运行记录与快照" {
		t.Fatalf("remark = %s", newCleanTask.Remark)
	}

	// 验证新表已创建
	if !testDB.Migrator().HasTable(&model.CollectRecord{}) {
		t.Fatal("expected collect_records table to exist")
	}
	if testDB.Migrator().HasTable(model.TableFailureRecord) {
		t.Fatal("expected failure_records to be dropped")
	}
}
