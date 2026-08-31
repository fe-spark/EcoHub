package model

import "gorm.io/gorm"

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

// BasicConfig 网站基本信息 (返回前端DTO与Redis缓存结构相同)
type BasicConfig struct {
	SiteName string `json:"siteName"` // 网站名称
	// SiteURL 网站访问地址（公网根地址，如 https://example.com），用于 Logo 跳转与 Telegram 播放链接等
	SiteURL  string    `json:"siteUrl"`
	Logo     string    `json:"logo"`     // 网站logo
	Keyword  string    `json:"keyword"`  // seo关键字
	Describe string    `json:"describe"` // 网站描述信息
	State    bool         `json:"state"`    // 网站状态 开启 || 关闭
	Hint     string       `json:"hint"`     // 网站关闭提示
	Tip      TipConfig    `json:"tip"`      // 前台赞赏
	Notice   NoticeConfig `json:"notice"`   // 开屏公告
}

// Banner 首页横幅信息
type Banner struct {
	Id           string `gorm:"primaryKey;size:64" json:"id"` // 唯一标识
	Mid          int64  `gorm:"index" json:"mid"`             // 绑定所属影片Id
	Name         string `gorm:"size:128" json:"name"`         // 影片名称
	Year         int64  `json:"year"`                         // 上映年份
	CName        string `gorm:"size:64" json:"cName"`         // 分类名称
	Poster        string `gorm:"size:512" json:"poster"`        // 竖版海报图（最终展示）
	Picture       string `gorm:"size:512" json:"picture"`       // 竖版封面图（片库/海报源原图）
	PictureSlide  string `gorm:"size:512" json:"pictureSlide"`  // 横版幻灯图（片库/海报源原图）
	CustomPicture string `gorm:"size:512" json:"customPicture"` // 自定义封面图（独立存储）
	Remark        string `gorm:"size:128" json:"remark"`        // 更新状态描述信息
	Sort          int64  `json:"sort"`                          // 排序分値
	IsCustomPic   bool   `gorm:"default:false" json:"isCustomPic"` // 是否为人工自定义图片（锁定不被海报源覆盖）
}

func (Banner) TableName() string {
	return TableBanners
}

type Banners []Banner

func (bl Banners) Len() int           { return len(bl) }
func (bl Banners) Less(i, j int) bool { return bl[i].Sort < bl[j].Sort }
func (bl Banners) Swap(i, j int)      { bl[i], bl[j] = bl[j], bl[i] }

// ------------------------------------------------------ MySQL 持久化模型 ---

// SiteConfigRecord 网站基础配置持久化 (MySQL单行表)
type SiteConfigRecord struct {
	gorm.Model
	SiteName   string `gorm:"size:128"`
	SiteURL    string `gorm:"size:512;column:site_url"` // 网站访问地址
	Logo       string `gorm:"size:512"`
	Keyword    string `gorm:"size:256"`
	Describe   string `gorm:"size:512"`
	State      bool
	Hint       string `gorm:"size:512"`
	TipJSON    string `gorm:"type:text;column:tip_json"`    // TipConfig JSON
	NoticeJSON string `gorm:"type:text;column:notice_json"` // NoticeConfig JSON
}

// MappingRule 定义从采集源到标准系统的转换规则 (地区/语言/标签黑名单)
type MappingRule struct {
	gorm.Model
	Group     string `gorm:"uniqueIndex:uidx_group_raw_match_type;size:32" json:"group"`                   // Area, Language, Blacklist
	Raw       string `gorm:"uniqueIndex:uidx_group_raw_match_type;size:128" json:"raw"`                    // 原始值 (采集源)
	Target    string `gorm:"size:128" json:"target"`                                                       // 标准值 (如果为空则视为黑名单项)
	MatchType string `gorm:"uniqueIndex:uidx_group_raw_match_type;size:16;default:exact" json:"matchType"` // exact | regex
	Remarks   string `gorm:"size:256" json:"remarks"`                                                      // 备注
}

func (MappingRule) TableName() string {
	return "mapping_rules"
}
