package model

import "time"

// 轮播图片模式。follow 跟随片库封面；custom 用本行的覆盖图。
const (
	BannerImageFollow = "follow"
	BannerImageCustom = "custom"
)

// BannerSlot 一个采集站的一条轮播。展示字段在读取时从片库组装，这里只存排序和后台改过的覆盖项。
type BannerSlot struct {
	Id               string    `gorm:"primaryKey;size:64" json:"id"`
	SourceId         string    `gorm:"size:32;not null;uniqueIndex:uidx_banner_slot_source_mid,priority:1;index:idx_banner_slot_source_sort,priority:1" json:"sourceId"`
	Mid              int64     `gorm:"not null;uniqueIndex:uidx_banner_slot_source_mid,priority:2;index" json:"mid"`
	Sort             int64     `gorm:"not null;default:0;index:idx_banner_slot_source_sort,priority:2" json:"sort"`
	ImageMode        string    `gorm:"size:16;not null;default:'follow'" json:"imageMode"`
	PosterOverride   string    `gorm:"size:512" json:"posterOverride,omitempty"`
	BackdropOverride string    `gorm:"size:512" json:"backdropOverride,omitempty"`
	BlurbOverride    string    `gorm:"type:text" json:"blurbOverride,omitempty"`
	ScoreOverride    float64   `json:"scoreOverride,omitempty"`
	YearOverride     int64     `json:"yearOverride,omitempty"`
	TmdbID           int64     `json:"tmdbId,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

func (BannerSlot) TableName() string {
	return TableBannerSlot
}

// Banner 首页 /home 返回的轮播。Web、Android、鸿蒙按这些键读取，字段不要改。
type Banner struct {
	Id            string  `json:"id"`
	Mid           int64   `json:"mid"`
	Name          string  `json:"name"`
	Year          int64   `json:"year"`
	CName         string  `json:"cName"`
	Poster        string  `json:"poster"`
	Picture       string  `json:"picture"`
	PictureSlide  string  `json:"pictureSlide"`
	CustomPicture string  `json:"customPicture"`
	Remark        string  `json:"remark"`
	Sort          int64   `json:"sort"`
	IsCustomPic   bool    `json:"isCustomPic"`
	Area          string  `json:"area,omitempty"`
	ClassTag      string  `json:"classTag,omitempty"`
	Actor         string  `json:"actor,omitempty"`
	Director      string  `json:"director,omitempty"`
	Blurb         string  `json:"blurb,omitempty"`
	Score         float64 `json:"score,omitempty"`
	TmdbID        int64   `json:"tmdbId,omitempty"`
	IsScraped     bool    `json:"isScraped"`
}

type Banners []Banner

// BannerValues 一组可覆盖的展示值。片库值和覆盖值都用它。
type BannerValues struct {
	Poster   string  `json:"poster"`
	Backdrop string  `json:"backdrop"`
	Blurb    string  `json:"blurb"`
	Score    float64 `json:"score"`
	Year     int64   `json:"year"`
}

// BannerAdminItem 后台列表和排片预览的一行：组装后的展示字段，加上图片模式、覆盖值和片库值。
type BannerAdminItem struct {
	Banner
	SourceId  string       `json:"sourceId"`
	ImageMode string       `json:"imageMode"`
	Overrides BannerValues `json:"overrides"`
	Library   BannerValues `json:"library"`
}

// LegacyBanner 旧表 banners_record 和旧版备份里的一行，只用于迁移和导入旧备份。
type LegacyBanner struct {
	Id            string  `json:"id"`
	Mid           int64   `json:"mid"`
	Year          int64   `json:"year"`
	Poster        string  `json:"poster"`
	Picture       string  `json:"picture"`
	PictureSlide  string  `json:"pictureSlide"`
	CustomPicture string  `json:"customPicture"`
	Blurb         string  `json:"blurb"`
	Score         float64 `json:"score"`
	Sort          int64   `json:"sort"`
	IsCustomPic   bool    `json:"isCustomPic"`
	TmdbID        int64   `json:"tmdbId"`
}
