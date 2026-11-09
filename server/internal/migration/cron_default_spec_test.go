package migration

import (
	"testing"

	"server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMigrateCronDefaultSpecs_OnlyOldDefaults(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	// 表不存在时不报错。
	if err := migrateCronDefaultSpecs(gdb); err != nil {
		t.Fatalf("no table: %v", err)
	}
	if err := gdb.AutoMigrate(&model.CrontabRecord{}); err != nil {
		t.Fatal(err)
	}
	seed := []model.CrontabRecord{
		{TaskId: "sys_cron_orphan_clean", TaskModel: model.TaskModelOrphanClean, Spec: "0 0 2 * * *", State: true},
		{TaskId: "sys_cron_log_clean", TaskModel: model.TaskModelLogClean, Spec: "0 15 4 * * *", State: true}, // 用户自定义
		{TaskId: model.TaskIDCollectClean, TaskModel: model.TaskModelCollectClean, Spec: "0 30 3 * * *", State: false},
		{TaskId: "user_task", TaskModel: model.TaskModelOrphanClean, Spec: "0 0 2 * * *", State: true}, // 非系统任务
	}
	for i := range seed {
		if err := gdb.Create(&seed[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateCronDefaultSpecs(gdb); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"sys_cron_orphan_clean":  "0 10 2 * * *",
		"sys_cron_log_clean":     "0 15 4 * * *",
		model.TaskIDCollectClean: "0 40 3 * * *",
		"user_task":              "0 0 2 * * *",
	}
	var rows []model.CrontabRecord
	gdb.Find(&rows)
	for _, r := range rows {
		if r.Spec != want[r.TaskId] {
			t.Fatalf("%s spec = %q, want %q", r.TaskId, r.Spec, want[r.TaskId])
		}
	}
}
