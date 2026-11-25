package repobase

import (
	"errors"
	"fmt"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/migration"
	"server/internal/model"

	"gorm.io/gorm"
)

// AutoMigrateAll 按当前模型建表。
func AutoMigrateAll() error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	return db.Mdb.AutoMigrate(model.AllModels...)
}

// RunSchemaMigrations 执行版本迁移。
func RunSchemaMigrations() error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	return migration.RunAutoMigrations(db.Mdb)
}

// AlignUserAutoIncrement 把用户表自增起点对齐到内置管理员 ID。
func AlignUserAutoIncrement() error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	return db.Mdb.Exec(fmt.Sprintf("alter table %s auto_Increment = %d", model.TableUser, config.UserIdInitialVal)).Error
}

// TruncateTable 统一跨数据库（MySQL TRUNCATE 与 SQLite DELETE）表截断与清空方言
func TruncateTable(conn *gorm.DB, table string) error {
	if conn == nil {
		return nil
	}
	if conn.Dialector != nil && conn.Dialector.Name() == "sqlite" {
		if !conn.Migrator().HasTable(table) {
			return nil
		}
		if err := conn.Exec(fmt.Sprintf("DELETE FROM %s", table)).Error; err != nil {
			return fmt.Errorf("truncate (sqlite delete) %s failed: %w", table, err)
		}
		return nil
	}
	if err := conn.Exec(fmt.Sprintf("TRUNCATE table %s", table)).Error; err != nil {
		return fmt.Errorf("truncate %s failed: %w", table, err)
	}
	return nil
}
