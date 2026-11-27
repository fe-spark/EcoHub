package model

import "time"

// HomeRecommendGroup 首页推荐分组（按采集站独立管理）。
type HomeRecommendGroup struct {
	Id           string     `gorm:"primaryKey;size:64" json:"id"`
	SourceId     string     `gorm:"size:32;not null;index:idx_rec_group_src_sort,priority:1" json:"sourceId"`
	Title        string     `gorm:"size:64;not null" json:"title"`
	Sort         int64      `gorm:"not null;default:0;index:idx_rec_group_src_sort,priority:2" json:"sort"`
	Enabled      bool       `gorm:"not null;default:true" json:"enabled"`
	AutoSync     bool       `gorm:"not null;default:false" json:"autoSync"`
	SyncChartKey string     `gorm:"size:32;not null;default:''" json:"syncChartKey"`
	CategoryId   int64      `gorm:"not null;default:0" json:"categoryId"`
	SyncLimit    int64      `gorm:"not null;default:12" json:"syncLimit"`
	LastSyncedAt *time.Time `json:"lastSyncedAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

func (HomeRecommendGroup) TableName() string {
	return TableHomeRecommendGroup
}

// HomeRecommendItem 首页推荐分组下的单部影片。
type HomeRecommendItem struct {
	Id          string    `gorm:"primaryKey;size:64" json:"id"`
	GroupId     string    `gorm:"size:64;not null;uniqueIndex:uidx_rec_item_group_mid,priority:1;index:idx_rec_item_group_sort,priority:1" json:"groupId"`
	SourceId    string    `gorm:"size:32;not null;index" json:"sourceId"`
	Mid         int64     `gorm:"not null;uniqueIndex:uidx_rec_item_group_mid,priority:2;index" json:"mid"`
	Sort        int64     `gorm:"not null;default:0;index:idx_rec_item_group_sort,priority:2" json:"sort"`
	DoubanScore string    `gorm:"size:16;not null;default:''" json:"doubanScore,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (HomeRecommendItem) TableName() string {
	return TableHomeRecommendItem
}

// HomeRecommendPublicItem 前台首页推荐卡片展示项。
type HomeRecommendPublicItem struct {
	MovieBasicInfo
	DoubanScore string `json:"doubanScore,omitempty"`
	Rank        int    `json:"rank"`
}

// HomeRecommendSection 前台首页统一展示楼层（推荐分组与站点分类按后台顺序输出）。
type HomeRecommendSection struct {
	Id           string                    `json:"id"`
	Kind         string                    `json:"kind"` // recommend | category
	Title        string                    `json:"title"`
	AutoSync     bool                      `json:"autoSync"`
	SyncChartKey string                    `json:"syncChartKey,omitempty"`
	CategoryId   int64                     `json:"categoryId,omitempty"`
	Nav          *CategoryTree             `json:"nav,omitempty"`
	Items        []HomeRecommendPublicItem `json:"items"`
	Hot          []MovieBasicInfo          `json:"hot,omitempty"`
}

// HomeRecommendAdminItem 后台管理的推荐条目。
type HomeRecommendAdminItem struct {
	ItemId         string  `json:"itemId"`
	GroupId        string  `json:"groupId"`
	SourceId       string  `json:"sourceId"`
	Mid            int64   `json:"mid"`
	Sort           int64   `json:"sort"`
	DoubanScore    string  `json:"doubanScore,omitempty"`
	Name           string  `json:"name"`
	Picture        string  `json:"picture"`
	Year           string  `json:"year"`
	CName          string  `json:"cName"`
	Area           string  `json:"area"`
	Remarks        string  `json:"remarks"`
	Score          float64 `json:"score,omitempty"`
	Unavailable    bool    `json:"unavailable,omitempty"`
	CategoryHidden bool    `json:"categoryHidden,omitempty"`
}

// HomeRecommendAdminGroup 后台管理的推荐分组详情。
type HomeRecommendAdminGroup struct {
	HomeRecommendGroup
	CategoryName   string                   `json:"categoryName,omitempty"`
	CategoryHidden bool                     `json:"categoryHidden,omitempty"`
	Items          []HomeRecommendAdminItem `json:"items"`
}
