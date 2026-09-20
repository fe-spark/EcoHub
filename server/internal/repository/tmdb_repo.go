package repository

import (
	"encoding/json"
	"log"
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
		Proxy:       "",
		Language:    DefaultTMDBLanguage,
		ImageDomain: DefaultTMDBImageDomain,
	}
}

// NormalizeTMDBConfig 规范化配置
func NormalizeTMDBConfig(cfg model.TMDBConfig) model.TMDBConfig {
	cfg.ApiKey = strings.TrimSpace(cfg.ApiKey)
	cfg.Proxy = strings.TrimSpace(cfg.Proxy)
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

// GetTMDBConfig 读取 TMDB 配置（Redis 缓存优先，MySQL 兜底）
func GetTMDBConfig() model.TMDBConfig {
	cfg := DefaultTMDBConfig()
	if db.Rdb != nil {
		if data := db.Rdb.Get(db.Cxt, config.TMDBConfigKey).Val(); data != "" {
			if err := json.Unmarshal([]byte(data), &cfg); err == nil {
				return NormalizeTMDBConfig(cfg)
			}
		}
	}

	if db.Mdb == nil {
		return cfg
	}

	var rec model.TMDBConfigRecord
	if err := db.Mdb.Order("id DESC").First(&rec).Error; err != nil {
		return cfg
	}
	raw := strings.TrimSpace(rec.Payload)
	if raw == "" {
		return cfg
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return cfg
	}
	cfg = NormalizeTMDBConfig(cfg)
	cacheTMDBConfig(cfg)
	return cfg
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
	cacheTMDBConfig(cfg)
	return nil
}

func cacheTMDBConfig(cfg model.TMDBConfig) {
	if db.Rdb == nil {
		return
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return
	}
	if err := db.Rdb.Set(db.Cxt, config.TMDBConfigKey, data, config.ConfigCacheTTL).Err(); err != nil {
		log.Println("SaveTMDBConfig Redis Error:", err)
		_ = db.Rdb.Del(db.Cxt, config.TMDBConfigKey).Err()
	}
}
