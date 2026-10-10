package model

import "gorm.io/gorm"

// ProxyModuleScope 细粒度生效模块配置
type ProxyModuleScope struct {
	Spider  bool `json:"spider"`  // 采集站爬虫
	TMDB    bool `json:"tmdb"`    // TMDB 刮削
	Notify  bool `json:"notify"`  // Telegram 通知
	Upgrade bool `json:"upgrade"` // GitHub 版本检查
}

// ProxyConfig 统一网络代理配置
type ProxyConfig struct {
	Enabled  bool             `json:"enabled"`  // 是否启用代理
	ProxyURL string           `json:"proxyUrl"` // 代理地址 (http://, https://, socks5://)
	Modules  ProxyModuleScope `json:"modules"`  // 各模块代理开关
}

// ProxyConfigRecord 代理配置持久化模型 (MySQL)
type ProxyConfigRecord struct {
	gorm.Model
	Payload string `gorm:"type:text"`
}

func (ProxyConfigRecord) TableName() string {
	return TableProxyConfig
}

// ProxyTestReq 连通测试请求（移除自定义 Target 彻底杜绝 SSRF）
type ProxyTestReq struct {
	ProxyURL string `json:"proxyUrl"`
}
