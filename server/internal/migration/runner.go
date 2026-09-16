package migration

import (
	"fmt"
	"strings"
	"time"

	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"

	"gorm.io/gorm"
)

// Migration 定义单个数据库版本迁移任务
type Migration struct {
	Version string
	Name    string
	Run     func(db *gorm.DB) error
}

// migrations 注册所有历史迁移任务（严格按时间戳版本递增顺序执行）
var migrations = []Migration{
	{
		Version: "20260301_mapping_rule_indexes",
		Name:    "normalize mapping_rules unique index and match_type",
		Run:     migrateMappingRuleIndexes,
	},
	{
		Version: "20260401_snapshot_performance_indexes",
		Name:    "create composite performance indexes for film_list_snapshot",
		Run:     migrateSnapshotPerformanceIndexes,
	},
	{
		Version: "20260501_purge_synced_gallery",
		Name:    "purge historical synced gallery pictures and records once",
		Run:     migratePurgeSyncedGallery,
	},
	{
		Version: "20260916_fix_movie_match_key_indexes",
		Name:    "rebuild idx_match_key on movie_match_key to single column index",
		Run:     migrateMovieMatchKeyIndexes,
	},
}

// RunAutoMigrations 顺序执行尚未执行的历史版本迁移，并持久化到 schema_migrations 表
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

func migrateMappingRuleIndexes(db *gorm.DB) error {
	return repository.EnsureMappingRuleIndexes()
}

func migrateSnapshotPerformanceIndexes(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.FilmListSnapshot{}) {
		return nil
	}
	queries := []string{
		"CREATE INDEX idx_snap_pid_update ON film_list_snapshot(snapshot_version, pid, update_stamp)",
		"CREATE INDEX idx_snap_cid_update ON film_list_snapshot(snapshot_version, cid, update_stamp)",
		"CREATE INDEX idx_snap_pid_hits ON film_list_snapshot(snapshot_version, pid, hits)",
		"CREATE INDEX idx_snap_cid_hits ON film_list_snapshot(snapshot_version, cid, hits)",
		"CREATE INDEX idx_snap_pid_year ON film_list_snapshot(snapshot_version, pid, year, update_stamp)",
		"CREATE INDEX idx_snap_ver_hits_pid ON film_list_snapshot(snapshot_version, hits, pid)",
		"CREATE INDEX idx_snap_ver_series ON film_list_snapshot(snapshot_version, series_key, update_stamp)",
	}
	for _, sql := range queries {
		if err := db.Exec(sql).Error; err != nil {
			msg := strings.ToLower(err.Error())
			if !strings.Contains(msg, "duplicate key name") && !strings.Contains(msg, "already exists") {
				return err
			}
		}
	}
	return nil
}

func migratePurgeSyncedGallery(db *gorm.DB) error {
	repository.PurgeSyncedGallery()
	return nil
}

func migrateMovieMatchKeyIndexes(db *gorm.DB) error {
	migrator := db.Migrator()
	if !migrator.HasTable(&model.MovieMatchKey{}) {
		return nil
	}

	rebuild := false
	if migrator.HasIndex(&model.MovieMatchKey{}, "idx_match_key") {
		var colName string
		err := db.Raw(`
			SELECT COLUMN_NAME 
			FROM INFORMATION_SCHEMA.STATISTICS 
			WHERE TABLE_SCHEMA = DATABASE() 
			  AND TABLE_NAME = ? 
			  AND INDEX_NAME = 'idx_match_key' 
			ORDER BY SEQ_IN_INDEX ASC 
			LIMIT 1
		`, model.TableMovieMatchKey).Scan(&colName).Error
		if err == nil && colName != "" && !strings.EqualFold(colName, "match_key") {
			rebuild = true
		}
	} else {
		rebuild = true
	}

	if rebuild {
		_ = migrator.DropIndex(&model.MovieMatchKey{}, "idx_match_key")
		if err := migrator.CreateIndex(&model.MovieMatchKey{}, "idx_match_key"); err != nil {
			return err
		}
	}
	return nil
}
