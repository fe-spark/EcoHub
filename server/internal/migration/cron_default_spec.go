package migration

import (
	"time"

	"server/internal/model"

	"gorm.io/gorm"
)

// cronDefaultSpecChange 系统维护任务默认时刻从整点挪开（避免与整点自动采集同秒撞车）。
type cronDefaultSpecChange struct {
	TaskID    string
	TaskModel int
	OldSpec   string
	NewSpec   string
}

var cronDefaultSpecChanges = []cronDefaultSpecChange{
	{TaskID: "sys_cron_orphan_clean", TaskModel: model.TaskModelOrphanClean, OldSpec: "0 0 2 * * *", NewSpec: "0 10 2 * * *"},
	{TaskID: "sys_cron_log_clean", TaskModel: model.TaskModelLogClean, OldSpec: "0 0 3 * * *", NewSpec: "0 10 3 * * *"},
	{TaskID: model.TaskIDCollectClean, TaskModel: model.TaskModelCollectClean, OldSpec: "0 30 3 * * *", NewSpec: "0 40 3 * * *"},
}

// migrateCronDefaultSpecs 只更新仍是旧默认时刻的系统任务；用户改过的时刻保持不变。
func migrateCronDefaultSpecs(gdb *gorm.DB) error {
	if gdb == nil || !gdb.Migrator().HasTable(&model.CrontabRecord{}) {
		return nil
	}
	now := time.Now()
	for _, c := range cronDefaultSpecChanges {
		if err := gdb.Model(&model.CrontabRecord{}).
			Where("task_id = ? AND task_model = ? AND spec = ?", c.TaskID, c.TaskModel, c.OldSpec).
			Updates(map[string]any{"spec": c.NewSpec, "updated_at": now}).Error; err != nil {
			return err
		}
	}
	return nil
}
