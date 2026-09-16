package model

import "time"

// SchemaMigration 记录已执行的数据库迁移版本，防止补丁 DDL 与清洗逻辑在每次启动时重复执行
type SchemaMigration struct {
	Version   string    `gorm:"primaryKey;column:version;type:varchar(128);not null" json:"version"`
	Name      string    `gorm:"column:name;type:varchar(255);not null;default:''" json:"name"`
	AppliedAt time.Time `gorm:"column:applied_at;type:datetime;not null" json:"applied_at"`
}

func (SchemaMigration) TableName() string {
	return TableSchemaMigration
}
