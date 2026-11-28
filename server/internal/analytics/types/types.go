package types

import "time"

// 行为动作常量
const (
	ActionSearch   = "search"
	ActionPlay     = "play"
	ActionBrowse   = "browse"
	ActionClassify = "classify"
	ActionProvide  = "provide"
	ActionConfig   = "config"
)

// 模块与平台常量
const (
	ModuleAll   = "all"
	ModuleWeb   = "web"
	ModuleApp   = "app"
	ModuleTvbox = "tvbox"
)

// AppPlatforms 支持的移动端平台枚举
var AppPlatforms = []string{"android", "harmony", "ios"}

// Event 采集到的单条访问或页面埋点事件
type Event struct {
	Ts              time.Time `json:"ts"`
	Node            string    `json:"node,omitempty"`
	Method          string    `json:"method"`
	Path            string    `json:"path"`
	Route           string    `json:"route"`
	Action          string    `json:"action"`
	Status          int       `json:"status"`
	LatencyMs       int64     `json:"latencyMs"`
	ClientType      string    `json:"clientType"`
	Internal        string    `json:"internal,omitempty"`
	IPHash          string    `json:"-"`
	IPPreview       string    `json:"ipPreview"`
	UAFamily        string    `json:"uaFamily"`
	Resource        string    `json:"resource"`
	ResourceTitle   string    `json:"resourceTitle,omitempty"`
	ResourcePoster  string    `json:"resourcePoster,omitempty"`
	ResourceCat     string    `json:"resourceCat,omitempty"`
	ResourceHref    string    `json:"resourceHref,omitempty"`
	Page            string    `json:"page,omitempty"`
	PageTitle       string    `json:"pageTitle,omitempty"`
	AppVersion      string    `json:"appVersion,omitempty"`
	DeviceModel     string    `json:"deviceModel,omitempty"`
	DeviceId        string    `json:"deviceId,omitempty"`
	OS              string    `json:"os,omitempty"`
	Query           string    `json:"query,omitempty"`
	CollectSourceId string    `json:"collectSourceId,omitempty"`
	PlayMember      string    `json:"-"`
	UVMember        string    `json:"-"`
}

// TrackViewPayload 客户端页面埋点请求体
type TrackViewPayload struct {
	Action         string `json:"action"`
	Resource       string `json:"resource"`
	ResourceTitle  string `json:"resource_title"`
	ResourcePoster string `json:"resource_poster"`
	ResourceCat    string `json:"resource_cat"`
	Source         string `json:"source"`
	CollectSource  string `json:"collect_source"`
	Path           string `json:"path"`
	Page           string `json:"page"`
	PageTitle      string `json:"page_title"`
	AppVersion     string `json:"app_version"`
	DeviceModel    string `json:"device_model"`
	DeviceId       string `json:"device_id"`
}

// Overview 单日概览数据（支持全站与分端/分平台范围）
type Overview struct {
	Day       string           `json:"day"`
	PV        int64            `json:"pv"`
	UV        int64            `json:"uv"`
	Err4      int64            `json:"err4"`
	Err5      int64            `json:"err5"`
	P95Ms     int64            `json:"p95Ms"`
	Dropped   int64            `json:"dropped"`
	Provide   ProvideStats     `json:"provide"`
	Client    map[string]int64 `json:"client"`
	Action    map[string]int64 `json:"action"`
	Hist      map[string]int64 `json:"hist"`
	Series    []SeriesPoint    `json:"series"`
	Platforms map[string]int64 `json:"platforms,omitempty"`
	Versions  map[string]int64 `json:"versions,omitempty"`
	Browsers  map[string]int64 `json:"browsers,omitempty"`
	Models    map[string]int64 `json:"models,omitempty"`
	OS        map[string]int64 `json:"os,omitempty"`
}

// ProvideStats TVBox 订阅接口汇总指标
type ProvideStats struct {
	PV   int64 `json:"pv"`
	Err4 int64 `json:"err4"`
	Err5 int64 `json:"err5"`
}

// SeriesPoint 15 分钟时间片指标点
type SeriesPoint struct {
	T         string `json:"t"`
	PV        int64  `json:"pv"`
	Err4      int64  `json:"err4"`
	Err5      int64  `json:"err5"`
	ProvidePV int64  `json:"providePv"`
	WebPV     int64  `json:"webPv,omitempty"`
	AppPV     int64  `json:"appPv,omitempty"`
	AndroidPV int64  `json:"androidPv,omitempty"`
	HarmonyPV int64  `json:"harmonyPv,omitempty"`
	IosPV     int64  `json:"iosPv,omitempty"`
}

// TopItem 排行榜单条目
type TopItem struct {
	Key      string `json:"key"`
	Count    int64  `json:"count"`
	Title    string `json:"title,omitempty"`
	Category string `json:"category,omitempty"`
	Poster   string `json:"poster,omitempty"`
	Year     int64  `json:"year,omitempty"`
	Href     string `json:"href,omitempty"`
}

// TopPageResult 榜单分页查询响应
type TopPageResult struct {
	Kind     string    `json:"kind"`
	Items    []TopItem `json:"items"`
	Total    int       `json:"total"`
	CappedAt int       `json:"cappedAt"`
}

// LogPageResult 访问流水分页查询响应
type LogPageResult struct {
	List  []Event `json:"list"`
	Total int     `json:"total"`
}

// SourceCallRow 采集站单日调用次数
type SourceCallRow struct {
	Id        string `json:"id"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	IsPrimary bool   `json:"isPrimary"`
	Count     int64  `json:"count"`
}

// DashboardPayload 看板聚合查询响应，减少前端并发请求数
type DashboardPayload struct {
	Day           string          `json:"day"`
	Module        string          `json:"module"`
	Platform      string          `json:"platform"`
	Global        *Overview       `json:"global"`
	Scoped        *Overview       `json:"scoped"`
	Sources       []SourceCallRow `json:"sources"`
	PlayTops      []TopItem       `json:"playTops"`
	SearchTops    []TopItem       `json:"searchTops"`
	ClassifyTops  []TopItem       `json:"classifyTops"`
	PageTops      []TopItem       `json:"pageTops"`
	Logs          []Event         `json:"logs"`
	LogsTotal     int             `json:"logsTotal"`
	AvailableDays []string        `json:"availableDays"`
}

// DataStats 数据分析数据积累统计
type DataStats struct {
	DailyStatsCount int64  `json:"dailyStatsCount"`
	DailyTopCount   int64  `json:"dailyTopCount"`
	RedisKeyCount   int64  `json:"redisKeyCount"`
	EarliestDay     string `json:"earliestDay,omitempty"`
	LatestDay       string `json:"latestDay,omitempty"`
	TotalPV         int64  `json:"totalPv"`
	TotalUV         int64  `json:"totalUv"`
}

// ClearResult 数据清理结果
type ClearResult struct {
	DeletedDailyStats int64 `json:"deletedDailyStats"`
	DeletedDailyTop   int64 `json:"deletedDailyTop"`
	DeletedRedisKeys  int64 `json:"deletedRedisKeys"`
}

// StatusInfo 开关与持久化数据概览
type StatusInfo struct {
	Enabled   bool  `json:"enabled"`
	HasData   bool  `json:"hasData"`
	TotalRows int64 `json:"totalRows"`
}
