package model

import "gorm.io/gorm"

// AnalyticsConfig 数据分析与行为审计配置
type AnalyticsConfig struct {
	Enabled bool `json:"enabled"` // 是否开启全站数据分析与播放行为统计
}

// DefaultAnalyticsConfig 默认关闭，保持最高性能与零开销
func DefaultAnalyticsConfig() AnalyticsConfig {
	return AnalyticsConfig{
		Enabled: false,
	}
}

// AnalyticsConfigRecord 数据分析配置持久化模型 (MySQL)
type AnalyticsConfigRecord struct {
	gorm.Model
	Payload string `gorm:"type:text"`
}

func (AnalyticsConfigRecord) TableName() string {
	return TableAnalyticsConfig
}
