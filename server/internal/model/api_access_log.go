package model

import "time"

// ApiAccessLog 记录 HTTP 接口请求的访问与耗时日志
// 采用异步缓冲批量落库 + 定时滑动窗口保留近 7 天，物理锁定数据库与运存上限
type ApiAccessLog struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	CreatedAt  time.Time `gorm:"index:idx_api_log_created_status,priority:1;not null" json:"createdAt"`
	Method     string    `gorm:"type:varchar(8);not null" json:"method"`
	Path       string    `gorm:"type:varchar(191);not null" json:"path"`
	Query      string    `gorm:"type:varchar(500)" json:"query"`
	Status     int       `gorm:"index:idx_api_log_created_status,priority:2;not null" json:"status"`
	DurationMs int64     `gorm:"not null" json:"durationMs"`
	IP         string    `gorm:"type:varchar(45)" json:"ip"`
	ClientType string    `gorm:"type:varchar(16)" json:"clientType"`
	DeviceId   string    `gorm:"type:varchar(64)" json:"deviceId"`
	UA         string    `gorm:"type:varchar(255)" json:"ua"`
}

func (ApiAccessLog) TableName() string {
	return TableApiAccessLog
}
