package model

import "time"

// WebdavMediaGroup 媒体分组（电影单文件一组，电视剧按季独立成组）
type WebdavMediaGroup struct {
	ID        uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	SourceId  string `gorm:"size:32;uniqueIndex:uidx_wdv_group" json:"sourceId"`
	GroupKey  string `gorm:"size:191;uniqueIndex:uidx_wdv_group" json:"groupKey"`
	GlobalMid int64  `gorm:"index" json:"globalMid"` // 命中的主站 mid；非 unique（多源或多季可挂同一片）
	TmdbId    int64  `gorm:"index" json:"tmdbId"`
	TmdbType  string `gorm:"size:16" json:"tmdbType"`
	Title     string `gorm:"size:255" json:"title"`
	Year      int64  `json:"year"`
}

func (WebdavMediaGroup) TableName() string {
	return TableWebdavMediaGroup
}

// WebdavScanItem 单个视频文件扫描指纹与状态
type WebdavScanItem struct {
	ID           uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	SourceId     string `gorm:"size:32;uniqueIndex:uidx_wdv_path" json:"sourceId"`
	PathHash     string `gorm:"size:40;uniqueIndex:uidx_wdv_path" json:"pathHash"` // sha1(relPath)
	RelPath      string `gorm:"type:text" json:"relPath"`
	Size         int64  `json:"size"`
	LastModified string `gorm:"size:64" json:"lastModified"`
	Fingerprint  string `gorm:"size:64;index" json:"fingerprint"`
	GroupKey     string `gorm:"size:191;index" json:"groupKey"`
	Title        string `gorm:"size:255" json:"title"`
	Year         int64  `json:"year"`
	Season       int    `json:"season"`
	Episode      int    `json:"episode"`
	TmdbId       int64  `json:"tmdbId"`
	Status       string `gorm:"size:32;index" json:"status"` // pending|scraped|unmatched|skipped|missing|bound|parse_failed|category_mismatch|ambiguous
	Hint         string `gorm:"size:32" json:"hint"`         // disc_split|range_disc|unplayable_container
	LastError    string `gorm:"size:512" json:"lastError"`
}

func (WebdavScanItem) TableName() string {
	return TableWebdavScanItem
}

// WebdavScanReport WebDAV 扫描审计报告
type WebdavScanReport struct {
	ID           uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	SourceId     string    `gorm:"size:32;index" json:"sourceId"`
	StartedAt    time.Time `json:"startedAt"`
	FinishedAt   time.Time `json:"finishedAt"`
	Status       string    `gorm:"size:24" json:"status"`
	Found        int       `json:"found"`
	Parsed       int       `json:"parsed"`
	TmdbHit      int       `json:"tmdbHit"`
	Unmatched    int       `json:"unmatched"`
	Skipped      int       `json:"skipped"`
	Saved        int       `json:"saved"`
	Deleted      int       `json:"deleted"`
	Failed       int       `json:"failed"`
	Truncated    bool      `json:"truncated"`
	ErrorSummary string    `gorm:"size:512" json:"errorSummary"`
}

func (WebdavScanReport) TableName() string {
	return TableWebdavScanReport
}

// WebdavBindRequest 手动指定 TMDB 绑定或多行合并为剧请求
type WebdavBindRequest struct {
	SourceId  string   `json:"sourceId" binding:"required"`
	ItemIds   []uint64 `json:"itemIds" binding:"required"`
	TmdbId    int64    `json:"tmdbId" binding:"required"`
	MediaType string   `json:"mediaType" binding:"required"` // "movie" | "tv"
}

// WebdavRescrapeRequest 重新刮削请求
type WebdavRescrapeRequest struct {
	SourceId string   `json:"sourceId" binding:"required"`
	ItemId   uint64   `json:"itemId,omitempty"`
	ItemIds  []uint64 `json:"itemIds,omitempty"`
	GroupKey string   `json:"groupKey,omitempty"`
}
