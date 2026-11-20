package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// GetAnalyticsConfig 读取数据分析配置。优先读取 Redis 缓存；无缓存查库；数据库无记录返回默认关闭。
func GetAnalyticsConfig() (model.AnalyticsConfig, error) {
	cfg, err := loadCachedConfig(
		config.AnalyticsConfigKey,
		decodeAnalyticsConfig,
		func() (model.AnalyticsConfig, string, bool, error) {
			if db.Mdb == nil {
				return model.DefaultAnalyticsConfig(), "", false, nil
			}
			var rec model.AnalyticsConfigRecord
			if err := db.Mdb.Order("id DESC").First(&rec).Error; err != nil {
				if recordNotFound(err) {
					return model.DefaultAnalyticsConfig(), "", false, nil
				}
				return model.DefaultAnalyticsConfig(), "", false, err
			}
			cfg, err := decodeAnalyticsConfig(rec.Payload)
			if err != nil {
				return model.DefaultAnalyticsConfig(), "", false, err
			}
			data, err := json.Marshal(cfg)
			if err != nil {
				return model.DefaultAnalyticsConfig(), "", false, err
			}
			return cfg, string(data), true, nil
		},
	)
	if err != nil {
		return model.DefaultAnalyticsConfig(), err
	}
	return cfg, nil
}

// SaveAnalyticsConfig 事务写入 MySQL，并同步刷新 Redis 缓存
func SaveAnalyticsConfig(cfg model.AnalyticsConfig) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	rec := model.AnalyticsConfigRecord{Payload: string(raw)}
	err = db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.AnalyticsConfigRecord{}).Error; err != nil {
			return err
		}
		return tx.Create(&rec).Error
	})
	if err != nil {
		return err
	}
	return cacheAnalyticsConfig(cfg)
}

func decodeAnalyticsConfig(raw string) (model.AnalyticsConfig, error) {
	if strings.TrimSpace(raw) == "" {
		return model.DefaultAnalyticsConfig(), nil
	}
	var cfg model.AnalyticsConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return model.DefaultAnalyticsConfig(), fmt.Errorf("数据分析配置无法解析: %w", err)
	}
	return cfg, nil
}

func cacheAnalyticsConfig(cfg model.AnalyticsConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return writeCache(config.AnalyticsConfigKey, string(data))
}
