package repository

import (
	"errors"
	"fmt"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/migration"
	"server/internal/model"
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
