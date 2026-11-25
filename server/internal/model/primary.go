package model

import "time"

// PrimaryState 全站主站状态（单行，Id 恒为 1）。
// 主站由后台显式切换，排序不改变主站；不并入 SiteConfigRecord（保存站点配置会整表重建）。
type PrimaryState struct {
	Id              int64  `gorm:"primaryKey"` // 恒为 1
	PrimarySourceId string `gorm:"size:32"`
	UpdatedAt       time.Time
}

func (PrimaryState) TableName() string {
	return TablePrimaryState
}
