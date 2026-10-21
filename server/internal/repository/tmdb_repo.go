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

const (
	DefaultTMDBLanguage    = "zh-CN"
	DefaultTMDBImageDomain = "https://image.tmdb.org/t/p"
)

// DefaultTMDBConfig 返回默认 TMDB 配置
func DefaultTMDBConfig() model.TMDBConfig {
	return model.TMDBConfig{
		Enabled:     false,
		ApiKey:      "",
		Language:    DefaultTMDBLanguage,
		ImageDomain: DefaultTMDBImageDomain,
	}
}

// NormalizeTMDBConfig 规范化配置
func NormalizeTMDBConfig(cfg model.TMDBConfig) model.TMDBConfig {
	cfg.ApiKey = strings.TrimSpace(cfg.ApiKey)
	cfg.Language = strings.TrimSpace(cfg.Language)
	if cfg.Language == "" {
		cfg.Language = DefaultTMDBLanguage
	}
	cfg.ImageDomain = strings.TrimSpace(cfg.ImageDomain)
	if cfg.ImageDomain == "" {
		cfg.ImageDomain = DefaultTMDBImageDomain
	}
	cfg.ImageDomain = strings.TrimRight(cfg.ImageDomain, "/")
	return cfg
}

// MaskTMDBApiKey 密钥脱敏展示
func MaskTMDBApiKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	r := []rune(key)
	n := len(r)
	if n <= 8 {
		return strings.Repeat("*", n)
	}
	return string(r[:4]) + "***" + string(r[n-4:])
}

// IsMaskedTMDBApiKey 判断是否为脱敏后的占位 key
func IsMaskedTMDBApiKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	if strings.Trim(key, "*") == "" {
		return true
	}
	if len([]rune(key)) > 8 && strings.Contains(key, "***") {
		parts := strings.Split(key, "***")
		if len(parts) == 2 && len([]rune(parts[0])) == 4 && len([]rune(parts[1])) == 4 {
			return true
		}
	}
	return false
}

// PublicTMDBConfig 返回前端展示的脱敏配置
func PublicTMDBConfig(cfg model.TMDBConfig) model.TMDBConfig {
	cfg.ApiKey = MaskTMDBApiKey(cfg.ApiKey)
	return cfg
}

// GetTMDBConfig 读取 TMDB 配置。没有记录时返回默认值，不写入缓存。
func GetTMDBConfig() (model.TMDBConfig, error) {
	if data, ok, err := readCache(config.TMDBConfigKey); err != nil {
		return model.TMDBConfig{}, err
	} else if ok && data != "" {
		cfg, err := decodeTMDBConfig(data)
		if err != nil {
			if delErr := dropCache(config.TMDBConfigKey); delErr != nil {
				return model.TMDBConfig{}, fmt.Errorf("%w; 删除缓存失败: %v", err, delErr)
			}
		} else {
			return cfg, nil
		}
	}
	if db.Mdb == nil {
		return model.TMDBConfig{}, errors.New("数据库未连接")
	}
	var rec model.TMDBConfigRecord
	if err := db.Mdb.Order("id DESC").First(&rec).Error; err != nil {
		if recordNotFound(err) {
			return DefaultTMDBConfig(), nil
		}
		return model.TMDBConfig{}, err
	}
	cfg, err := decodeTMDBConfig(rec.Payload)
	if err != nil {
		return model.TMDBConfig{}, err
	}
	if err := cacheTMDBConfig(cfg); err != nil {
		return model.TMDBConfig{}, err
	}
	return cfg, nil
}

func decodeTMDBConfig(raw string) (model.TMDBConfig, error) {
	if strings.TrimSpace(raw) == "" {
		return DefaultTMDBConfig(), nil
	}
	var cfg model.TMDBConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return model.TMDBConfig{}, fmt.Errorf("TMDB 配置无法解析: %w", err)
	}
	return NormalizeTMDBConfig(cfg), nil
}

// SaveTMDBConfig 持久化 TMDB 配置并刷新 Redis 缓存
func SaveTMDBConfig(cfg model.TMDBConfig) error {
	cfg = NormalizeTMDBConfig(cfg)
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	rec := model.TMDBConfigRecord{Payload: string(raw)}
	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.TMDBConfigRecord{}).Error; err != nil {
			return err
		}
		return tx.Create(&rec).Error
	}); err != nil {
		return err
	}
	return cacheTMDBConfig(cfg)
}

func cacheTMDBConfig(cfg model.TMDBConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return writeCache(config.TMDBConfigKey, string(data))
}
