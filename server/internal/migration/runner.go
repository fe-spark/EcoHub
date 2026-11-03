package migration

import (
	"fmt"
	"time"

	"server/internal/infra/syslog"
	"server/internal/model"

	"gorm.io/gorm"
)

// Migration 定义单个数据库版本迁移。
type Migration struct {
	Version string
	Name    string
	Run     func(db *gorm.DB) error
}

// migrations 按版本号递增执行。已发布版本不要改 Run，后续升级在末尾追加。
// 当前大版本没有历史补丁，只留一条空基线。
var migrations = []Migration{
	{
		Version: "20261009_baseline",
		Name:    "当前版本基线，无结构变更",
		Run:     migrateBaseline,
	},
	{
		Version: "20261102_collect_records_refactor",
		Name:    "采集记录体系重构：创建 collect_records 表，废除旧重试任务并初始化采集记录清理任务",
		Run:     migrateCollectRecordsRefactor,
	},
	{
		Version: "20261102_drop_failure_records",
		Name:    "删除 failure_records",
		Run:     migrateDropFailureRecords,
	},
	{
		Version: "20261103_collect_record_films",
		Name:    "创建 collect_record_films 表，支持采集入库影片全量清单与异步分页检索",
		Run:     migrateCollectRecordFilms,
	},
	{
		Version: "20261104_collect_record_films_unique",
		Name:    "collect_record_films 加 (batch_id, source_id, mid) 唯一索引，重复结算不再累加",
		Run:     migrateCollectRecordFilmsUnique,
	},
}

func migrateBaseline(*gorm.DB) error {
	return nil
}

func migrateCollectRecordsRefactor(db *gorm.DB) error {
	// 1. 改表：确保 collect_records 与 crontab_records 表就绪
	if err := db.AutoMigrate(&model.CollectRecord{}, &model.CrontabRecord{}); err != nil {
		return fmt.Errorf("migrate tables failed: %w", err)
	}
	// 旧的独立重试任务下线。失败记录清理改成采集记录清理时保留开关和 cron。
	if err := db.Unscoped().Where("task_id = ?", "sys_cron_recover_collect").
		Delete(&model.CrontabRecord{}).Error; err != nil {
		return fmt.Errorf("cleanup deprecated cron tasks failed: %w", err)
	}

	const collectCleanRemark = "定期清理超过 30 天的采集运行记录与快照"
	var collectCleanCount int64
	if err := db.Model(&model.CrontabRecord{}).Where("task_id = ?", model.TaskIDCollectClean).Count(&collectCleanCount).Error; err != nil {
		return fmt.Errorf("query collect clean cron task failed: %w", err)
	}
	if collectCleanCount > 0 {
		if err := db.Unscoped().Where("task_id = ?", model.TaskIDFailureClean).Delete(&model.CrontabRecord{}).Error; err != nil {
			return fmt.Errorf("cleanup deprecated cron tasks failed: %w", err)
		}
		return nil
	}

	renamed := db.Model(&model.CrontabRecord{}).Where("task_id = ?", model.TaskIDFailureClean).Updates(map[string]any{
		"task_id":    model.TaskIDCollectClean,
		"task_model": model.TaskModelCollectClean,
		"remark":     collectCleanRemark,
	})
	if renamed.Error != nil {
		return fmt.Errorf("rename collect clean cron task failed: %w", renamed.Error)
	}
	if renamed.RowsAffected > 0 {
		return nil
	}

	collectCleanTask := model.CrontabRecord{
		TaskId:    model.TaskIDCollectClean,
		Time:      0,
		Spec:      "0 30 3 * * *",
		TaskModel: model.TaskModelCollectClean,
		State:     true,
		Remark:    collectCleanRemark,
	}
	if err := db.Create(&collectCleanTask).Error; err != nil {
		return fmt.Errorf("init collect clean cron task failed: %w", err)
	}
	return nil
}

func migrateDropFailureRecords(db *gorm.DB) error {
	if !db.Migrator().HasTable(model.TableFailureRecord) {
		return nil
	}
	if err := db.Migrator().DropTable(model.TableFailureRecord); err != nil {
		return fmt.Errorf("drop failure_records failed: %w", err)
	}
	return nil
}

func migrateCollectRecordFilms(db *gorm.DB) error {
	if err := db.AutoMigrate(&model.CollectRecordFilmItem{}); err != nil {
		return fmt.Errorf("migrate collect_record_films failed: %w", err)
	}
	return nil
}

// migrateCollectRecordFilmsUnique 给已建好的表补唯一索引。
// 20261103 已登记的库不会重跑 AutoMigrate，索引必须走独立版本。
func migrateCollectRecordFilmsUnique(db *gorm.DB) error {
	var keep []uint
	if err := db.Model(&model.CollectRecordFilmItem{}).
		Select("MIN(id)").
		Group("batch_id, source_id, mid").
		Scan(&keep).Error; err != nil {
		return fmt.Errorf("dedupe collect_record_films failed: %w", err)
	}
	if len(keep) > 0 {
		if err := db.Where("id NOT IN ?", keep).Delete(&model.CollectRecordFilmItem{}).Error; err != nil {
			return fmt.Errorf("dedupe collect_record_films failed: %w", err)
		}
	}
	if err := db.AutoMigrate(&model.CollectRecordFilmItem{}); err != nil {
		return fmt.Errorf("migrate collect_record_films unique index failed: %w", err)
	}
	return nil
}

// RunAutoMigrations 顺序执行尚未记录的迁移，并写入 schema_migrations。
func RunAutoMigrations(db *gorm.DB) error {
	if db == nil {
		return nil
	}

	if !db.Migrator().HasTable(&model.SchemaMigration{}) {
		if err := db.AutoMigrate(&model.SchemaMigration{}); err != nil {
			return fmt.Errorf("init schema_migrations table failed: %w", err)
		}
	}

	var applied []string
	if err := db.Model(&model.SchemaMigration{}).Pluck("version", &applied).Error; err != nil {
		return fmt.Errorf("load applied migrations failed: %w", err)
	}

	appliedSet := make(map[string]bool, len(applied))
	for _, v := range applied {
		appliedSet[v] = true
	}

	newApplied := 0
	for _, m := range migrations {
		if appliedSet[m.Version] {
			continue
		}
		if m.Run == nil {
			return fmt.Errorf("migration %s has no runner", m.Version)
		}

		syslog.Infof("[Migration] 正在执行版本迁移 %s (%s)...", m.Version, m.Name)
		if err := m.Run(db); err != nil {
			syslog.Errorf("[Migration] %s 执行失败: %v", m.Version, err)
			return fmt.Errorf("migration %s failed: %w", m.Version, err)
		}

		record := model.SchemaMigration{
			Version:   m.Version,
			Name:      m.Name,
			AppliedAt: time.Now(),
		}
		if err := db.Create(&record).Error; err != nil {
			return fmt.Errorf("record migration %s failed: %w", m.Version, err)
		}
		syslog.Infof("[Migration] 成功完成版本迁移 %s", m.Version)
		newApplied++
	}

	if newApplied > 0 {
		syslog.Infof("[Migration] 本次成功执行 %d 项版本迁移，数据库当前已就绪 (累计 %d 项)", newApplied, len(appliedSet)+newApplied)
	} else {
		syslog.Infof("[Migration] 数据库版本化迁移已是最新 (已应用 %d 项历史迁移，无需执行)", len(appliedSet))
	}
	return nil
}
