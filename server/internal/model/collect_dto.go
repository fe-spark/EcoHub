package model

import "time"

// FilmSourceUpsertRequest 采集源新增/编辑请求体
type FilmSourceUpsertRequest struct {
	Id                 string        `json:"id"`
	Name               string        `json:"name"`
	Uri                string        `json:"uri"` // MacCMS 用；WebDAV 忽略，由服务端根据 serverUrl 和 rootPath 合成
	Grade              SourceGrade   `json:"grade"`
	State              bool          `json:"state"`
	IsPosterSource     bool          `json:"isPosterSource"`
	Interval           int           `json:"interval"`
	Cd                 int           `json:"cd"`
	DomainReplaceRules string        `json:"domainReplaceRules"`
	SourceType         SourceType    `json:"sourceType"`
	Webdav             *WebdavUpsert `json:"webdav"`
}

// WebdavUpsert WebDAV 配置增量/全量更新参数
type WebdavUpsert struct {
	ServerURL       string `json:"serverUrl"`
	Username        string `json:"username"`
	Password        string `json:"password"` // 留空表示不修改
	RootPath        string `json:"rootPath"`
	MediaType       string `json:"mediaType"` // "movie" | "tv"
	TmdbApiKey      string `json:"tmdbApiKey"` // 留空表示不修改
	TmdbBaseURL     string `json:"tmdbBaseUrl"`
	ScanIntervalMin int    `json:"scanIntervalMin"`
	MinFileBytes    int64  `json:"minFileBytes"`
	PlayFromName    string `json:"playFromName"`
}

// FilmSourceTestRequest 采集源连通性测试请求体
type FilmSourceTestRequest struct {
	Id         string        `json:"id"` // 列表「测试连通」只传 id，服务端读库
	SourceType SourceType    `json:"sourceType"`
	Name       string        `json:"name"`
	Uri        string        `json:"uri"`
	Webdav     *WebdavUpsert `json:"webdav"`
}

// WebdavConfigPublic WebDAV 公开脱敏配置（用于管理后台列表展示）
type WebdavConfigPublic struct {
	ServerURL       string `json:"serverUrl"`
	Username        string `json:"username"`
	RootPath        string `json:"rootPath"`
	MediaType       string `json:"mediaType"`
	TmdbBaseURL     string `json:"tmdbBaseUrl"`
	PlayFromName    string `json:"playFromName"`
	PasswordSet     bool   `json:"passwordSet"`   // 密码是否已设置（不返回明文）
	TmdbApiKeySet   bool   `json:"tmdbApiKeySet"` // TMDB Key 是否已设置（不返回明文）
	ScanIntervalMin int    `json:"scanIntervalMin"`
	MinFileBytes    int64  `json:"minFileBytes"`
}

// WebdavScanSummary WebDAV 采集卡片扫描概括信息
type WebdavScanSummary struct {
	Found        int        `json:"found"`
	TmdbHit      int        `json:"tmdbHit"`
	Unmatched    int        `json:"unmatched"`
	Skipped      int        `json:"skipped"`
	Status       string     `json:"status,omitempty"`
	ErrorSummary string     `json:"errorSummary,omitempty"`
	LastScan     *time.Time `json:"lastScan,omitempty"`
}

// FilmSourceListItemPublic 采集站列表公开脱敏展示项
type FilmSourceListItemPublic struct {
	Id                 string              `json:"id"`
	Name               string              `json:"name"`
	Uri                string              `json:"uri"`
	Grade              SourceGrade         `json:"grade"`
	State              bool                `json:"state"`
	IsPosterSource     bool                `json:"isPosterSource"`
	Interval           int                 `json:"interval"`
	Cd                 int                 `json:"cd"`
	DomainReplaceRules string              `json:"domainReplaceRules"`
	SourceType         SourceType          `json:"sourceType"`
	LastCollectTime    *time.Time          `json:"lastCollectTime,omitempty"`
	Progress           *CollectProgress    `json:"progress,omitempty"`
	Webdav             *WebdavConfigPublic `json:"webdav,omitempty"`
	ScanSummary        *WebdavScanSummary  `json:"scanSummary,omitempty"`
}
