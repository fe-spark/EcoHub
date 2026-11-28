package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	appcfg "server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repobase"

	"gorm.io/gorm"
)

var enabledState atomic.Bool

// IsEnabled 纳秒级原子读取数据分析总开关状态
func IsEnabled() bool {
	return enabledState.Load()
}

// SetEnabled 热更新内存原子开关状态
func SetEnabled(val bool) {
	enabledState.Store(val)
}

// GetConfig 读取数据分析配置。优先读取 Redis 缓存；无缓存查库；无记录返回默认关闭。
func GetConfig() (model.AnalyticsConfig, error) {
	cfg, err := repobase.LoadCachedConfig(
		appcfg.AnalyticsConfigKey,
		decodeConfig,
		func() (model.AnalyticsConfig, string, bool, error) {
			if db.Mdb == nil {
				return model.DefaultAnalyticsConfig(), "", false, nil
			}
			var rec model.AnalyticsConfigRecord
			if err := db.Mdb.Order("id DESC").First(&rec).Error; err != nil {
				if repobase.RecordNotFound(err) {
					return model.DefaultAnalyticsConfig(), "", false, nil
				}
				return model.DefaultAnalyticsConfig(), "", false, err
			}
			cfg, err := decodeConfig(rec.Payload)
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

// SaveConfig 事务持久化配置至 MySQL，并同步清理配置缓存
func SaveConfig(cfg model.AnalyticsConfig) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	err = db.Mdb.Transaction(func(tx *gorm.DB) error {
		var existing model.AnalyticsConfigRecord
		findErr := tx.Order("id ASC").First(&existing).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			return tx.Create(&model.AnalyticsConfigRecord{Payload: string(raw)}).Error
		}
		if findErr != nil {
			return findErr
		}
		return tx.Model(&model.AnalyticsConfigRecord{}).
			Where("id = ?", existing.ID).
			Update("payload", string(raw)).Error
	})
	if err != nil {
		return err
	}
	return repobase.DropConfigCache(appcfg.AnalyticsConfigKey)
}

// GetAndSync 读取数据分析配置并同步内存原子状态
func GetAndSync() (model.AnalyticsConfig, error) {
	cfg, err := GetConfig()
	if err != nil {
		return model.DefaultAnalyticsConfig(), err
	}
	SetEnabled(cfg.Enabled)
	return cfg, nil
}

// UpdateAndSync 更新数据分析配置并立即热生效
func UpdateAndSync(cfg model.AnalyticsConfig) error {
	if err := SaveConfig(cfg); err != nil {
		return err
	}
	SetEnabled(cfg.Enabled)
	return nil
}

func decodeConfig(raw string) (model.AnalyticsConfig, error) {
	if strings.TrimSpace(raw) == "" {
		return model.DefaultAnalyticsConfig(), nil
	}
	var cfg model.AnalyticsConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return model.DefaultAnalyticsConfig(), fmt.Errorf("数据分析配置无法解析: %w", err)
	}
	return cfg, nil
}
