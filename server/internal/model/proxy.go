package model

import "gorm.io/gorm"

const (
	ProxyScopeAll    = "all"    // 全部采集源生效
	ProxyScopeCustom = "custom" // 仅指定采集源生效
)

// ProxyConfig 采集网络代理配置
type ProxyConfig struct {
	Enabled   bool     `json:"enabled"`   // 是否启用代理
	ProxyURL  string   `json:"proxyUrl"`  // 代理地址 (http://, https://, socks5://)
	Scope     string   `json:"scope"`     // 生效范围: all | custom
	SourceIds []string `json:"sourceIds"` // 指定生效的采集源 ID 列表 (Scope == custom 时有效)
}

// ProxyConfigRecord 代理配置持久化模型 (MySQL)
type ProxyConfigRecord struct {
	gorm.Model
	Payload string `gorm:"type:text"`
}

func (ProxyConfigRecord) TableName() string {
	return TableProxyConfig
}

// ProxyTestReq 连通测试请求
type ProxyTestReq struct {
	ProxyURL string `json:"proxyUrl"`
	Target   string `json:"target,omitempty"` // 可选自定义测试目标
}
