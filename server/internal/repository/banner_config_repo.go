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
	BannerStrategyHot    = "hot_random"
	BannerStrategyScore  = "score_random"
	BannerStrategyLatest = "latest_random"
)

// DefaultBannerConfig 返回未保存时的排片配方。
func DefaultBannerConfig() model.BannerConfig {
	return model.BannerConfig{
		Strategy: BannerStrategyHot,
		Quotas:   []model.BannerQuota{},
	}
}

// NormalizeBannerConfig 规范化轮播配置。只保留当前配方字段。
func NormalizeBannerConfig(cfg model.BannerConfig) model.BannerConfig {
	cfg.Strategy = strings.ToLower(strings.TrimSpace(cfg.Strategy))
	switch cfg.Strategy {
	case BannerStrategyHot, BannerStrategyScore, BannerStrategyLatest:
	default:
		cfg.Strategy = BannerStrategyHot
	}
	cfg.Quotas = normalizeBannerQuotas(cfg.Quotas)
	return cfg
}

func normalizeBannerQuotas(quotas []model.BannerQuota) []model.BannerQuota {
	if len(quotas) == 0 {
		return []model.BannerQuota{}
	}
	out := make([]model.BannerQuota, 0, len(quotas))
	seen := make(map[int64]struct{}, len(quotas))
	for _, quota := range quotas {
		if quota.CategoryID <= 0 {
			continue
		}
		if _, ok := seen[quota.CategoryID]; ok {
			continue
		}
		seen[quota.CategoryID] = struct{}{}
		if quota.Count <= 0 {
			continue
		}
		out = append(out, model.BannerQuota{CategoryID: quota.CategoryID, Count: quota.Count})
	}
	return out
}

var (
	memBannerConfigLock sync.RWMutex
	memBannerConfig     *model.BannerConfig
)

// DeleteBannerConfig 删除已存排片配方。
func DeleteBannerConfig() error {
	memBannerConfigLock.Lock()
	memBannerConfig = nil
	memBannerConfigLock.Unlock()

	if db.Rdb != nil {
		if err := db.Rdb.Del(db.Cxt, config.BannerConfigKey).Err(); err != nil {
			_ = db.Rdb.Set(db.Cxt, config.BannerConfigKey, "null", config.ConfigCacheTTL).Err()
		}
	}
	if db.Mdb == nil {
		return nil
	}
	return db.Mdb.Unscoped().Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.BannerConfigRecord{}).Error
}

// BannerConfigExists 是否已经保存过一键排片配方。
func BannerConfigExists() bool {
	if db.Rdb != nil {
		n, err := db.Rdb.Exists(db.Cxt, config.BannerConfigKey).Result()
		if err == nil && n > 0 {
			return true
		}
	}
	if db.Mdb == nil {
		memBannerConfigLock.RLock()
		defer memBannerConfigLock.RUnlock()
		return memBannerConfig != nil
	}
	var count int64
	if err := db.Mdb.Model(&model.BannerConfigRecord{}).Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

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
