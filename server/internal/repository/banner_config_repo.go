package repository

import (
	"encoding/json"
	"log"
	"strings"
	"sync"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

const (
	BannerModeManual        = "manual"
	BannerModeAuto          = "auto"
	BannerStrategyHot       = "hot_random"
	BannerStrategyScore     = "score_random"
	BannerStrategyLatest    = "latest_random"
	BannerStrategySmartMix  = "smart_mix"
	DefaultBannerCount      = model.DefaultBannerCount
	MaxBannerCount          = model.MaxBannerCount
	DefaultBannerRefreshCron = "0 0 */12 * * *"
)

// DefaultBannerConfig 返回默认轮播配置
func DefaultBannerConfig() model.BannerConfig {
	return model.BannerConfig{
		Mode:        BannerModeManual,
		Strategy:    BannerStrategyHot,
		Count:       DefaultBannerCount,
		AutoTMDB:    true,
		RefreshCron: DefaultBannerRefreshCron,
		PinnedMids:  []int64{},
		Categories:  []int64{},
	}
}

// NormalizeBannerConfig 规范化轮播配置
func NormalizeBannerConfig(cfg model.BannerConfig) model.BannerConfig {
	cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	if cfg.Mode != BannerModeAuto {
		cfg.Mode = BannerModeManual
	}

	cfg.Strategy = strings.ToLower(strings.TrimSpace(cfg.Strategy))
	switch cfg.Strategy {
	case BannerStrategyHot, BannerStrategyScore, BannerStrategyLatest, BannerStrategySmartMix:
	default:
		cfg.Strategy = BannerStrategyHot
	}

	if cfg.Count <= 0 {
		cfg.Count = DefaultBannerCount
	} else if cfg.Count > MaxBannerCount {
		cfg.Count = MaxBannerCount
	}

	cfg.RefreshCron = strings.TrimSpace(cfg.RefreshCron)
	if cfg.RefreshCron == "" {
		cfg.RefreshCron = DefaultBannerRefreshCron
	}

	if cfg.PinnedMids == nil {
		cfg.PinnedMids = []int64{}
	}
	if cfg.Categories == nil {
		cfg.Categories = []int64{}
	}
	return cfg
}

var (
	memBannerConfigLock sync.RWMutex
	memBannerConfig     *model.BannerConfig
)

// GetBannerConfig 读取轮播排片配置（Redis 缓存优先，MySQL 兜底）
func GetBannerConfig() model.BannerConfig {
	cfg := DefaultBannerConfig()
	if db.Rdb != nil {
		if data := db.Rdb.Get(db.Cxt, config.BannerConfigKey).Val(); data != "" && data != "null" {
			if err := json.Unmarshal([]byte(data), &cfg); err == nil {
				return NormalizeBannerConfig(cfg)
			}
		}
	}

	if db.Mdb == nil {
		memBannerConfigLock.RLock()
		defer memBannerConfigLock.RUnlock()
		if memBannerConfig != nil {
			return NormalizeBannerConfig(*memBannerConfig)
		}
		return cfg
	}

	var rec model.BannerConfigRecord
	if err := db.Mdb.Order("id DESC").First(&rec).Error; err != nil {
		return cfg
	}
	raw := strings.TrimSpace(rec.Payload)
	if raw == "" {
		return cfg
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		log.Println("[BannerConfig] 解析配置 JSON 异常:", err)
		return cfg
	}
	cfg = NormalizeBannerConfig(cfg)
	cacheBannerConfig(cfg)
	return cfg
}

// SaveBannerConfig 持久化轮播排片配置并同步刷新 Redis 缓存
func SaveBannerConfig(cfg model.BannerConfig) error {
	cfg = NormalizeBannerConfig(cfg)
	memBannerConfigLock.Lock()
	memBannerConfig = &cfg
	memBannerConfigLock.Unlock()

	if db.Mdb == nil {
		return nil
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	rec := model.BannerConfigRecord{Payload: string(raw)}
	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.BannerConfigRecord{}).Error; err != nil {
			return err
		}
		return tx.Create(&rec).Error
	}); err != nil {
		return err
	}
	cacheBannerConfig(cfg)
	return nil
}

func cacheBannerConfig(cfg model.BannerConfig) {
	if db.Rdb == nil {
		return
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return
	}
	_ = db.Rdb.Set(db.Cxt, config.BannerConfigKey, string(data), config.ConfigCacheTTL).Err()
}
