package model

import "time"

// AccessDailyStats 访问分析按日滚动汇总，只保留近 14 天，不按请求增长。
type AccessDailyStats struct {
	Day            string    `json:"day" gorm:"size:10;primaryKey"`
	PV             int64     `json:"pv"`
	UV             int64     `json:"uv"`
	WebPV          int64     `json:"webPv"`
	WebUV          int64     `json:"webUv"`
	AppPV          int64     `json:"appPv"`
	AppUV          int64     `json:"appUv"`
	Err4           int64     `json:"err4"`
	Err5           int64     `json:"err5"`
	P95Ms          int64     `json:"p95Ms"`
	Dropped        int64     `json:"dropped"`
	ProvidePV      int64     `json:"providePv"`
	ProvideUV      int64     `json:"provideUv"`
	ProvideErr4    int64     `json:"provideErr4"`
	ProvideErr5    int64     `json:"provideErr5"`
	ClientJSON     string    `json:"-" gorm:"type:text"`
	ActionJSON     string    `json:"-" gorm:"type:text"`
	HistJSON       string    `json:"-" gorm:"type:text"`
	SeriesJSON     string    `json:"-" gorm:"type:text"`
	PlatformJSON   string    `json:"-" gorm:"type:text"`
	PlatformUVJSON string    `json:"-" gorm:"type:text"`
	VersionJSON    string    `json:"-" gorm:"type:text"`
	BrowserJSON    string    `json:"-" gorm:"type:text"`
	OSJSON         string    `json:"-" gorm:"type:text"`
	ModelsJSON     string    `json:"-" gorm:"type:text"`
	RolledAt       time.Time `json:"rolledAt"`
}

func (AccessDailyStats) TableName() string {
	return TableAccessDailyStats
}

// AccessDailyTop 访问分析按日 Top，每种 kind 每天最多 10 行。
type AccessDailyTop struct {
	Day      string `json:"day" gorm:"size:10;primaryKey"`
	Kind     string `json:"kind" gorm:"size:16;primaryKey"`
	Rank     int    `json:"rank" gorm:"primaryKey"`
	ItemKey  string `json:"itemKey" gorm:"size:512"`
	Count    int64  `json:"count"`
	Title    string `json:"title" gorm:"size:256"`
	Category string `json:"category" gorm:"size:128"`
	Poster   string `json:"poster" gorm:"size:512"`
	Year     int64  `json:"year"`
}

func (AccessDailyTop) TableName() string {
	return TableAccessDailyTop
}
