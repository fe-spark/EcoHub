package repository

import (
	"encoding/json"
	"fmt"
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

// DefaultBannerConfig 返回未保存时的排片配置。
func DefaultBannerConfig() model.BannerConfig {
	return model.BannerConfig{
		Strategy: BannerStrategyHot,
		Quotas:   []model.BannerQuota{},
	}
}

// NormalizeBannerConfig 规范化轮播排片配置。
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

// DeleteBannerConfig 删除已存排片配置。
func DeleteBannerConfig() error {
	memBannerConfigLock.Lock()
	memBannerConfig = nil
	memBannerConfigLock.Unlock()

	if err := dropCache(config.BannerConfigKey); err != nil {
		return err
	}
	if db.Mdb == nil {
		return nil
	}
	return db.Mdb.Unscoped().Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.BannerConfigRecord{}).Error
}

// BannerConfigExists 是否已经保存过排片配置。
func BannerConfigExists() (bool, error) {
	_, exists, err := LoadBannerConfig()
	return exists, err
}

// GetBannerConfig 读取轮播排片配置。没有记录时返回默认配置，不写入缓存。
func GetBannerConfig() (model.BannerConfig, error) {
	cfg, _, err := LoadBannerConfig()
	return cfg, err
}

// LoadBannerConfig 读取轮播排片配置及是否存在记录。
func LoadBannerConfig() (model.BannerConfig, bool, error) {
	if data, ok, err := readCache(config.BannerConfigKey); err != nil {
		return model.BannerConfig{}, false, err
	} else if ok && data != "" && data != "null" {
		cfg, err := decodeBannerConfig(data)
		if err != nil {
			if delErr := dropCache(config.BannerConfigKey); delErr != nil {
				return model.BannerConfig{}, false, fmt.Errorf("%w; 删除缓存失败: %v", err, delErr)
			}
		} else {
			return cfg, true, nil
		}
	}
	if db.Mdb == nil {
		memBannerConfigLock.RLock()
		defer memBannerConfigLock.RUnlock()
		if memBannerConfig != nil {
			return *memBannerConfig, true, nil
		}
		return DefaultBannerConfig(), false, nil
	}
	var rec model.BannerConfigRecord
	if err := db.Mdb.Order("id DESC").First(&rec).Error; err != nil {
		if recordNotFound(err) {
			return DefaultBannerConfig(), false, nil
		}
		return model.BannerConfig{}, false, err
	}
	cfg, err := decodeBannerConfig(rec.Payload)
	if err != nil {
		return model.BannerConfig{}, false, err
	}
	if err := cacheBannerConfig(cfg); err != nil {
		return model.BannerConfig{}, false, err
	}
	return cfg, true, nil
}

func decodeBannerConfig(raw string) (model.BannerConfig, error) {
	if strings.TrimSpace(raw) == "" {
		return DefaultBannerConfig(), nil
	}
	var cfg model.BannerConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return model.BannerConfig{}, fmt.Errorf("排片配置无法解析: %w", err)
	}
	return NormalizeBannerConfig(cfg), nil
}

// SaveBannerConfig 持久化轮播排片配置并同步刷新 Redis 缓存
func SaveBannerConfig(cfg model.BannerConfig) error {
	cfg = NormalizeBannerConfig(cfg)
	memBannerConfigLock.Lock()
	stored := cfg
	memBannerConfig = &stored
	memBannerConfigLock.Unlock()
	return writeBannerConfigRecord(cfg)
}

// SaveBannerConfigIfEmpty 仅在还没有正数量配额时写入。已有配额则原样返回。
func SaveBannerConfigIfEmpty(cfg model.BannerConfig) (model.BannerConfig, bool, error) {
	cfg = NormalizeBannerConfig(cfg)
	memBannerConfigLock.Lock()
	defer memBannerConfigLock.Unlock()
	if memBannerConfig != nil && len(memBannerConfig.Quotas) > 0 {
		return *memBannerConfig, false, nil
	}
	if err := writeBannerConfigRecord(cfg); err != nil {
		return model.BannerConfig{}, false, err
	}
	stored := cfg
	memBannerConfig = &stored
	return stored, true, nil
}

func writeBannerConfigRecord(cfg model.BannerConfig) error {
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
	return cacheBannerConfig(cfg)
}

func cacheBannerConfig(cfg model.BannerConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return writeCache(config.BannerConfigKey, string(data))
}
