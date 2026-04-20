package model

import "gorm.io/gorm"

// BasicConfig 网站基本信息 (返回前端DTO与Redis缓存结构相同)
type BasicConfig struct {
	SiteName     string `json:"siteName"`     // 网站名称
	Logo         string `json:"logo"`         // 网站logo
	Keyword      string `json:"keyword"`      // seo关键字
	Describe     string `json:"describe"`     // 网站描述信息
	State        bool   `json:"state"`        // 网站状态 开启 || 关闭
	Hint         string `json:"hint"`         // 网站关闭提示
	IsVideoProxy bool   `json:"isVideoProxy"` // 是否启用视频播放代理 (服务器中转)
}

// Banner 首页横幅信息
type Banner struct {
	Id           string `gorm:"primaryKey;size:64" json:"id"`                 // 唯一标识
	Mid          int64  `gorm:"index" json:"mid"`                             // 绑定所属影片Id
	Name         string `gorm:"size:128" json:"name"`                         // 影片名称
	Year         int64  `json:"year"`                                         // 上映年份
	CName        string `gorm:"size:64" json:"cName"`                         // 分类名称
	Poster       string `gorm:"size:512" json:"poster"`                       // 竖版海报图
	Picture      string `gorm:"size:512" json:"picture"`                      // 竖版封面图
	PictureSlide string `gorm:"size:512" json:"pictureSlide"`                 // 横版幻灯图
	Remark       string `gorm:"size:128" json:"remark"`                       // 更新状态描述信息
	Sort         int64  `json:"sort"`                                         // 排序分値
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
	SiteName     string `gorm:"size:128"`
	Logo         string `gorm:"size:512"`
	Keyword      string `gorm:"size:256"`
	Describe     string `gorm:"size:512"`
	State        bool
	Hint         string `gorm:"size:512"`
	IsVideoProxy bool   // 是否启用视频播放代理
}

// MappingRule 定义从采集源到标准系统的转换规则 (地区/语言/标签黑名单)
type MappingRule struct {
	gorm.Model
	Group   string `gorm:"uniqueIndex:uidx_group_raw;size:32"`  // Area, Language, Blacklist
	Raw     string `gorm:"uniqueIndex:uidx_group_raw;size:128"` // 原始值 (采集源)
	Target  string `gorm:"size:128"`                            // 标准值 (如果为空则视为黑名单项)
	Remarks string `gorm:"size:256"`                            // 备注
}

func (MappingRule) TableName() string {
	return "mapping_rules"
}
