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

func requireProxyModules(raw []byte) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("代理配置无法解析: %w", err)
	}
	if _, ok := probe["modules"]; !ok {
		return errors.New("代理配置缺少 modules")
	}
	return nil
}

// NormalizeProxyConfig 规范化代理配置
func NormalizeProxyConfig(cfg model.ProxyConfig) model.ProxyConfig {
	cfg.ProxyURL = strings.TrimSpace(cfg.ProxyURL)
	return cfg
}

// GetProxyConfig 读取代理配置。没有记录时返回默认关闭，不写入缓存。
func GetProxyConfig() (model.ProxyConfig, error) {
	if data, ok, err := readCache(config.ProxyConfigKey); err != nil {
		return model.ProxyConfig{}, err
	} else if ok && data != "" {
		cfg, err := decodeProxyConfig([]byte(data))
		if err != nil {
			if delErr := dropCache(config.ProxyConfigKey); delErr != nil {
				return model.ProxyConfig{}, fmt.Errorf("%w; 删除缓存失败: %v", err, delErr)
			}
		} else {
			return cfg, nil
		}
	}
	if db.Mdb == nil {
		return model.ProxyConfig{}, errors.New("数据库未连接")
	}
	var rec model.ProxyConfigRecord
	if err := db.Mdb.Order("id desc").First(&rec).Error; err != nil {
		if recordNotFound(err) {
			return DefaultProxyConfig(), nil
		}
		return model.ProxyConfig{}, err
	}
	cfg, err := decodeProxyConfig([]byte(rec.Payload))
	if err != nil {
		return model.ProxyConfig{}, err
	}
	if err := cacheProxyConfig(cfg); err != nil {
		return model.ProxyConfig{}, err
	}
	return cfg, nil
}

func decodeProxyConfig(raw []byte) (model.ProxyConfig, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return model.ProxyConfig{}, errors.New("代理配置为空")
	}
	if err := requireProxyModules(raw); err != nil {
		return model.ProxyConfig{}, err
	}
	var cfg model.ProxyConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return model.ProxyConfig{}, fmt.Errorf("代理配置无法解析: %w", err)
	}
	return NormalizeProxyConfig(cfg), nil
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

	return cacheProxyConfig(cfg)
}

func cacheProxyConfig(cfg model.ProxyConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return writeCache(config.ProxyConfigKey, string(data))
}

func proxyChannel(cfg model.ProxyConfig, enabled bool) (bool, string) {
	if !cfg.Enabled || cfg.ProxyURL == "" || !enabled {
		return false, ""
	}
	return true, cfg.ProxyURL
}

// ResolveSpiderProxy 全局爬虫代理通道。配置读失败返回错误。
func ResolveSpiderProxy() (bool, string, error) {
	cfg, err := GetProxyConfig()
	if err != nil {
		return false, "", err
	}
	ok, proxyURL := proxyChannel(cfg, cfg.Modules.Spider)
	return ok, proxyURL, nil
}

// ResolveSourceProxy 指定采集源是否走代理。配置或站点查询失败返回错误。
func ResolveSourceProxy(sourceID string) (bool, string, error) {
	ok, proxyURL, err := ResolveSpiderProxy()
	if err != nil || !ok {
		return false, "", err
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return false, "", errors.New("采集站 ID 为空")
	}
	if db.Mdb == nil {
		return false, "", errors.New("数据库未连接")
	}
	var src model.FilmSource
	if err := db.Mdb.Select("proxy_collect").Where("id = ?", sourceID).First(&src).Error; err != nil {
		if recordNotFound(err) {
			return false, "", nil
		}
		return false, "", err
	}
	if src.ProxyCollect {
		return true, proxyURL, nil
	}
	return false, "", nil
}

// ResolveTMDBProxy 判断 TMDB 刮削是否启用代理
func ResolveTMDBProxy() (bool, string, error) {
	cfg, err := GetProxyConfig()
	if err != nil {
		return false, "", err
	}
	ok, proxyURL := proxyChannel(cfg, cfg.Modules.TMDB)
	return ok, proxyURL, nil
}

// ResolveNotifyProxy 判断 Telegram 通知是否启用代理
func ResolveNotifyProxy() (bool, string, error) {
	cfg, err := GetProxyConfig()
	if err != nil {
		return false, "", err
	}
	ok, proxyURL := proxyChannel(cfg, cfg.Modules.Notify)
	return ok, proxyURL, nil
}

// ResolveUpgradeProxy 判断 GitHub 版本更新检查是否启用代理
func ResolveUpgradeProxy() (bool, string, error) {
	cfg, err := GetProxyConfig()
	if err != nil {
		return false, "", err
	}
	ok, proxyURL := proxyChannel(cfg, cfg.Modules.Upgrade)
	return ok, proxyURL, nil
}
