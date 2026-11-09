package model

import (
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	TipChannelWeChat  = "wechat"
	TipChannelAlipay  = "alipay"
	TipChannelCustom  = "custom"
	MaxTipChannels    = 4
	MaxTipTitleLen    = 32
	MaxTipMessageLen  = 120
	MaxTipLabelLen    = 32
	MaxTipImageLen    = 512
	MaxTipLinkLen     = 512
	DefaultTipTitle   = "赞赏支持"
	DefaultTipMessage = "如果这个站对你有帮助，欢迎请作者喝杯咖啡"
)

// TipChannel 前台赞赏渠道（收款码或外链）
type TipChannel struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	QrImage string `json:"qrImage"`
	Link    string `json:"link"`
}

// TipConfig 前台赞赏展示配置
type TipConfig struct {
	Enabled  bool         `json:"enabled"`
	Title    string       `json:"title"`
	Message  string       `json:"message"`
	Channels []TipChannel `json:"channels"`
}

// DefaultTipConfig 关闭状态的默认赞赏配置（预置微信 / 支付宝空渠道）
func DefaultTipConfig() TipConfig {
	return TipConfig{
		Enabled: false,
		Title:   DefaultTipTitle,
		Message: DefaultTipMessage,
		Channels: []TipChannel{
			{Key: TipChannelWeChat, Label: "微信"},
			{Key: TipChannelAlipay, Label: "支付宝"},
		},
	}
}

// MediaProxyConfig 媒体图片边缘反向代理配置
type MediaProxyConfig struct {
	Enabled  bool   `json:"enabled"`  // 是否启用媒体图片边缘反向代理
	Template string `json:"template"` // 反向代理模板（留空则默认 https://i{hash}.wp.com/{url}）
}

// DefaultMediaProxyConfig 默认开启媒体图片反向代理
func DefaultMediaProxyConfig() MediaProxyConfig {
	return MediaProxyConfig{
		Enabled:  true,
		Template: "",
	}
}

func DecodeMediaProxyJSON(raw string) (MediaProxyConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultMediaProxyConfig(), nil
	}
	var c MediaProxyConfig
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return MediaProxyConfig{}, err
	}
	return c, nil
}

func EncodeMediaProxyJSON(c MediaProxyConfig) (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// BasicConfig 网站基本信息 (返回前端DTO与Redis缓存结构相同)
type BasicConfig struct {
	SiteName string `json:"siteName"` // 网站名称
	// SiteURL 网站访问地址（公网根地址，如 https://example.com），用于 Logo 跳转与外部引用链接等
	SiteURL       string           `json:"siteUrl"`
	Logo          string           `json:"logo"`          // 网站logo
	Keyword       string           `json:"keyword"`       // seo关键字
	Describe      string           `json:"describe"`      // 网站描述信息
	State         bool             `json:"state"`         // 网站状态 开启 || 关闭
	Hint          string           `json:"hint"`          // 网站关闭提示
	PrivateAccess bool             `json:"privateAccess"` // 私有化访问控制（仅登录可用）
	ProvideKey    string           `json:"provideKey"`    // TVBox/订阅专属访问密钥
	Danmaku       DanmakuConfig    `json:"danmaku"`       // 弹幕系统配置
	Tip           TipConfig        `json:"tip"`           // 前台赞赏
	Notice        NoticeConfig     `json:"notice"`        // 开屏公告
	MediaProxy    MediaProxyConfig `json:"mediaProxy"`    // 媒体图片边缘反代配置
}

// AccessConfig 网站访问控制配置（运行状态/维护提示/私有化/订阅密钥）
type AccessConfig struct {
	State         bool   `json:"state"`         // 网站状态 开启 || 关闭
	Hint          string `json:"hint"`          // 网站关闭提示
	PrivateAccess bool   `json:"privateAccess"` // 私有化访问控制（仅登录可用）
	ProvideKey    string `json:"provideKey"`    // TVBox/订阅专属访问密钥
}

// ------------------------------------------------------ MySQL 持久化模型 ---

// SiteConfigRecord 网站基础配置持久化 (MySQL单行表)
type SiteConfigRecord struct {
	gorm.Model
	SiteName      string `gorm:"size:128"`
	SiteURL       string `gorm:"size:512;column:site_url"` // 网站访问地址
	Logo          string `gorm:"size:512"`
	Keyword       string `gorm:"size:256"`
	Describe      string `gorm:"size:512"`
	State         bool
	Hint          string `gorm:"size:512"`
	PrivateAccess bool   `gorm:"default:false;column:private_access"`
	ProvideKey    string `gorm:"size:64;column:provide_key"`
	DanmakuJSON    string `gorm:"type:text;column:danmaku_json"`     // DanmakuConfig JSON
	TipJSON        string `gorm:"type:text;column:tip_json"`         // TipConfig JSON
	NoticeJSON     string `gorm:"type:text;column:notice_json"`      // NoticeConfig JSON
	MediaProxyJSON string `gorm:"type:text;column:media_proxy_json"` // MediaProxyConfig JSON
}

func (SiteConfigRecord) TableName() string {
	return TableSiteConfig
}

// MappingRule 定义从采集源到标准系统的转换规则 (地区/语言/标签黑名单)
type MappingRule struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	SourceId  string         `gorm:"uniqueIndex:uidx_source_group_raw_match_type;size:32;default:''" json:"sourceId"`     // 分类规则绑定采集站，其它规则为空
	Group     string         `gorm:"uniqueIndex:uidx_source_group_raw_match_type;size:32" json:"group"`                   // Area, Language, CategoryRoot
	Raw       string         `gorm:"uniqueIndex:uidx_source_group_raw_match_type;size:128" json:"raw"`                    // 原始值 (采集源)
	Target    string         `gorm:"size:128" json:"target"`                                                              // 标准值 (如果为空则视为黑名单项)
	MatchType string         `gorm:"uniqueIndex:uidx_source_group_raw_match_type;size:16;default:exact" json:"matchType"` // exact | regex
	Remarks   string         `gorm:"size:256" json:"remarks"`                                                             // 备注
}

func (MappingRule) TableName() string {
	return TableMappingRule
}

