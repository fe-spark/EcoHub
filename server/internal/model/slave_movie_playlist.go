package model

import (
	"time"
)

// SlaveMoviePlaylist 附属站播放列表专属持久化模型（与主站骨架物理隔离）。
// 包含两阶段观察期时间戳 OrphanMarkedAt，用于无锁防误删治理。
// 显式声明主键与时间戳，不包含 deleted_at，彻底硬删除规避软删除与唯一索引冲突。
type SlaveMoviePlaylist struct {
	ID             uint       `gorm:"primaryKey"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
	SourceId       string     `gorm:"type:varchar(64);uniqueIndex:uidx_slave_source_key_group"`
	MovieKey       string     `gorm:"type:varchar(255);uniqueIndex:uidx_slave_source_key_group;index:idx_slave_movie_key"`
	GroupIndex     int        `gorm:"uniqueIndex:uidx_slave_source_key_group"`
	GroupName      string     `gorm:"type:varchar(255)"`
	Content        string     `gorm:"type:longtext"`
	OrphanMarkedAt *time.Time `gorm:"index:idx_slave_orphan_marked"` // 观察期标记时间戳（NULL 表示正常活跃）
}

func (SlaveMoviePlaylist) TableName() string {
	return TableSlaveMoviePlaylist
}
