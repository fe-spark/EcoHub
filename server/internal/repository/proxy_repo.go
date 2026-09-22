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

// DefaultProxyConfig 默认代理配置
func DefaultProxyConfig() model.ProxyConfig {
	return model.ProxyConfig{
		Enabled:   false,
		ProxyURL:  "",
		Scope:     model.ProxyScopeAll,
		SourceIds: []string{},
	}
}

// NormalizeProxyConfig 规范化代理配置
func NormalizeProxyConfig(cfg model.ProxyConfig) model.ProxyConfig {
	cfg.ProxyURL = strings.TrimSpace(cfg.ProxyURL)
	cfg.Scope = strings.TrimSpace(cfg.Scope)
	if cfg.Scope != model.ProxyScopeCustom {
		cfg.Scope = model.ProxyScopeAll
	}

	seen := make(map[string]struct{}, len(cfg.SourceIds))
	cleanIds := make([]string, 0, len(cfg.SourceIds))
	for _, id := range cfg.SourceIds {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			cleanIds = append(cleanIds, id)
		}
	}
	cfg.SourceIds = cleanIds
	return cfg
}

// GetProxyConfig 获取代理配置（Redis 优先，MySQL 兜底）
func GetProxyConfig() model.ProxyConfig {
	cfg := DefaultProxyConfig()
	if db.Rdb != nil {
		if data := db.Rdb.Get(db.Cxt, config.ProxyConfigKey).Val(); data != "" {
			if err := json.Unmarshal([]byte(data), &cfg); err == nil {
				return NormalizeProxyConfig(cfg)
			}
		}
	}

	if db.Mdb == nil {
		return cfg
	}

	var rec model.ProxyConfigRecord
	if err := db.Mdb.Order("id desc").First(&rec).Error; err != nil {
		return cfg
	}

	if rec.Payload != "" {
		_ = json.Unmarshal([]byte(rec.Payload), &cfg)
	}

	cfg = NormalizeProxyConfig(cfg)
	cacheProxyConfig(cfg)
	return cfg
}

// SaveProxyConfig 保存代理配置并刷新缓存
func SaveProxyConfig(cfg model.ProxyConfig) error {
	cfg = NormalizeProxyConfig(cfg)
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	rec := model.ProxyConfigRecord{Payload: string(raw)}
	err = db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.ProxyConfigRecord{}).Error; err != nil {
			return err
		}
		return tx.Create(&rec).Error
	})
	if err != nil {
		return err
	}

	cacheProxyConfig(cfg)
	return nil
}

func cacheProxyConfig(cfg model.ProxyConfig) {
	if db.Rdb == nil {
		return
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return
	}
	if err := db.Rdb.Set(db.Cxt, config.ProxyConfigKey, data, config.ConfigCacheTTL).Err(); err != nil {
		log.Println("SaveProxyConfig Redis Error:", err)
		_ = db.Rdb.Del(db.Cxt, config.ProxyConfigKey).Err()
	}
}

// ResolveSourceProxy 判断指定采集站是否启用代理，返回是否启用及代理地址
func ResolveSourceProxy(sourceID string) (bool, string) {
	cfg := GetProxyConfig()
	if !cfg.Enabled || cfg.ProxyURL == "" {
		return false, ""
	}
	if cfg.Scope == model.ProxyScopeAll {
		return true, cfg.ProxyURL
	}
	sourceID = strings.TrimSpace(sourceID)
	for _, id := range cfg.SourceIds {
		if id == sourceID {
			return true, cfg.ProxyURL
		}
	}
	return false, ""
}

// IsSourceProxyConfigured 判断指定采集站是否在代理配置名单中（不论全局总开关是否开启）
func IsSourceProxyConfigured(sourceID string) bool {
	cfg := GetProxyConfig()
	sourceID = strings.TrimSpace(sourceID)
	for _, id := range cfg.SourceIds {
		if id == sourceID {
			return true
		}
	}
	return false
}
