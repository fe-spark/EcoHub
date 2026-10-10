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
		Enabled:  false,
		ProxyURL: "",
		Modules: model.ProxyModuleScope{
			Spider:  true,
			TMDB:    true,
			Notify:  true,
			Upgrade: true,
		},
	}
}

// applyMissingProxyModulesDefault 兼容历史 JSON 缺失 modules 时默认全部开启
func applyMissingProxyModulesDefault(raw []byte, cfg *model.ProxyConfig) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return
	}
	if _, ok := probe["modules"]; !ok {
		cfg.Modules = model.ProxyModuleScope{
			Spider:  true,
			TMDB:    true,
			Notify:  true,
			Upgrade: true,
		}
	}
}

// NormalizeProxyConfig 规范化代理配置
func NormalizeProxyConfig(cfg model.ProxyConfig) model.ProxyConfig {
	cfg.ProxyURL = strings.TrimSpace(cfg.ProxyURL)
	return cfg
}

// GetProxyConfig 获取代理配置（Redis 优先，MySQL 兜底）
func GetProxyConfig() model.ProxyConfig {
	cfg := DefaultProxyConfig()
	if db.Rdb != nil {
		if data := db.Rdb.Get(db.Cxt, config.ProxyConfigKey).Val(); data != "" {
			raw := []byte(data)
			if err := json.Unmarshal(raw, &cfg); err == nil {
				applyMissingProxyModulesDefault(raw, &cfg)
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

	raw := []byte(rec.Payload)
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &cfg)
		applyMissingProxyModulesDefault(raw, &cfg)
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

// ResolveSpiderProxy 全局爬虫代理通道判断：是否启用全局爬虫代理及代理地址
func ResolveSpiderProxy() (bool, string) {
	cfg := GetProxyConfig()
	if !cfg.Enabled || cfg.ProxyURL == "" || !cfg.Modules.Spider {
		return false, ""
	}
	return true, cfg.ProxyURL
}

// ResolveSourceProxy 判断指定采集源是否走代理（结合全局爬虫通道和源自身 ProxyCollect 开关）
func ResolveSourceProxy(sourceID string) (bool, string) {
	ok, proxyURL := ResolveSpiderProxy()
	if !ok {
		return false, ""
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || db.Mdb == nil {
		return false, ""
	}
	var src model.FilmSource
	if err := db.Mdb.Select("proxy_collect").Where("id = ?", sourceID).First(&src).Error; err != nil {
		return false, ""
	}
	if src.ProxyCollect {
		return true, proxyURL
	}
	return false, ""
}

// ResolveTMDBProxy 判断 TMDB 刮削是否启用代理
func ResolveTMDBProxy() (bool, string) {
	cfg := GetProxyConfig()
	if !cfg.Enabled || cfg.ProxyURL == "" || !cfg.Modules.TMDB {
		return false, ""
	}
	return true, cfg.ProxyURL
}

// ResolveNotifyProxy 判断 Telegram 通知是否启用代理
func ResolveNotifyProxy() (bool, string) {
	cfg := GetProxyConfig()
	if !cfg.Enabled || cfg.ProxyURL == "" || !cfg.Modules.Notify {
		return false, ""
	}
	return true, cfg.ProxyURL
}

// ResolveUpgradeProxy 判断 GitHub 版本更新检查是否启用代理
func ResolveUpgradeProxy() (bool, string) {
	cfg := GetProxyConfig()
	if !cfg.Enabled || cfg.ProxyURL == "" || !cfg.Modules.Upgrade {
		return false, ""
	}
	return true, cfg.ProxyURL
}
