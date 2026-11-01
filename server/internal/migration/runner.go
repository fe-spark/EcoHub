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
}

func migrateBaseline(*gorm.DB) error {
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
