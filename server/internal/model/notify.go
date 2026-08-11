package model

import "time"

// Severity 通知严重程度。
type Severity string

const (
	SeverityInfo     Severity = "INFO"
	SeverityNotice   Severity = "NOTICE"
	SeverityWarn     Severity = "WARN"
	SeverityError    Severity = "ERROR"
	SeverityCritical Severity = "CRITICAL"
)

// 通知领域分类 Category
const (
	CategoryCollect = "collect"
	CategoryCron    = "cron"
	CategoryAudit   = "audit"
)

// NotifyTarget 通知接收目标（支持 Chat ID 和 Telegram Topic Thread ID）。
type NotifyTarget struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	ChatID               string   `json:"chatId"`
	ThreadID             string   `json:"threadId,omitempty"` // Telegram Forum Topic Thread ID
	Enabled              bool     `json:"enabled"`
	MinLevel             Severity `json:"minLevel,omitempty"`
	SubscribedCategories []string `json:"subscribedCategories,omitempty"`
}

// NotifyQuietHours 免打扰时段设置。
type NotifyQuietHours struct {
	Enabled     bool       `json:"enabled"`
	Start       string     `json:"start"`       // HH:mm 格式，例如 "23:00"
	End         string     `json:"end"`         // HH:mm 格式，例如 "07:00"
	AllowLevels []Severity `json:"allowLevels"` // 静音期间仍允许穿透的等级，默认 ["ERROR", "CRITICAL"]
}

// NotifyConfig Telegram 通知配置（MySQL JSON + Redis 缓存）。
type NotifyConfig struct {
	Enabled  bool           `json:"enabled"`
	BotToken string         `json:"botToken"`
	ChatIDs  []string       `json:"chatIds"` // 向后兼容旧版
	Targets  []NotifyTarget `json:"targets"` // 新版路由目标

	Events NotifyEventSwitches `json:"events"`

	IncludeFilmDetails bool             `json:"includeFilmDetails"`
	OnlyNotifyOnUpdate bool             `json:"onlyNotifyOnUpdate"` // 无更新且无失败时跳过推送
	MaxFilmsInMessage  int              `json:"maxFilmsInMessage"`
	MinIntervalSec     int              `json:"minIntervalSec"`
	QuietHours         NotifyQuietHours `json:"quietHours"`
}

// 通知消息中影片数约束（分页每页条数上限），后端校验/默认值统一来自此处。
const (
	MaxFilmsInMessageCap     = 20
	DefaultMaxFilmsInMessage = 15
)

// NotifyEvent 统一事件模型 Envelope
type NotifyEvent struct {
	ID        string                 `json:"id"`
	Key       string                 `json:"key"`        // e.g. "collect_batch_summary"
	Category  string                 `json:"category"`   // e.g. "collect", "cron", "audit"
	Severity  Severity               `json:"severity"`   // e.g. "INFO", "WARN", "ERROR"
	Title     string                 `json:"title"`
	Summary   string                 `json:"summary"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// NotifyEventSwitches 各事件开关。
type NotifyEventSwitches struct {
	CollectBatchSummary   bool `json:"collectBatchSummary"`
	CollectSourceFailed   bool `json:"collectSourceFailed"`
	CollectFinalizeFailed bool `json:"collectFinalizeFailed"`
	CollectProgressStale  bool `json:"collectProgressStale"`
	CronTaskFailed        bool `json:"cronTaskFailed"`
	CronTaskDone          bool `json:"cronTaskDone"`
	SourceConfigChanged   bool `json:"sourceConfigChanged"`
}

// NotifyConfigRecord 通知配置持久化（单行表，Payload 为 NotifyConfig JSON）。
type NotifyConfigRecord struct {
	ID        uint `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	Payload   string `gorm:"type:text;not null"`
}

func (NotifyConfigRecord) TableName() string {
	return TableNotifyConfig
}

// SourceConfigChangeItem 批量源配置变更通知中的单条（源 + 变更描述），批量操作聚合发送用。
type SourceConfigChangeItem struct {
	SourceName string   `json:"sourceName"`
	SourceID   string   `json:"sourceId"`
	Changes    []string `json:"changes"`
}

// 采集通知 Trigger 常量。
const (
	NotifyTriggerManual       = "manual"
	NotifyTriggerCron         = "cron"
	NotifyTriggerSingleUpdate = "single_update"
	NotifyTriggerRecover      = "recover"
)

// 通知事件名（内部 key，与开关字段对应）。
const (
	NotifyEventCollectBatchSummary   = "collect_batch_summary"
	NotifyEventCollectSourceFailed   = "collect_source_failed"
	NotifyEventCollectFinalizeFailed = "collect_finalize_failed"
	NotifyEventCollectProgressStale  = "collect_progress_stale"
	NotifyEventCronTaskFailed        = "cron_task_failed"
	NotifyEventCronTaskDone          = "cron_task_done"
	NotifyEventSourceConfigChanged   = "source_config_changed"
)

// CollectBatchNotifyPayload 批次采集摘要载荷。
type CollectBatchNotifyPayload struct {
	Trigger            string               `json:"trigger"`
	SiteName           string               `json:"siteName"`
	StartedAt          time.Time            `json:"startedAt"`
	FinishedAt         time.Time            `json:"finishedAt"`
	DurationSec        int64                `json:"durationSec"`
	Sources            []SourceNotifyResult `json:"sources"`
	TotalSources       int                  `json:"totalSources"`
	SuccessSources     int                  `json:"successSources"`
	FailedSources      int                  `json:"failedSources"`
	TotalFilms         int                  `json:"totalFilms"`
	IncludeFilmDetails bool                 `json:"includeFilmDetails"`
	FinalizeError      string               `json:"finalizeError,omitempty"`
	// ChangeBatchID 批次标识，由 BuildBatchPayload 在同步阶段写入；异步发送只读此值。
	ChangeBatchID string `json:"-"`
}

// SourceNotifyResult 单源结果。
type SourceNotifyResult struct {
	SourceID    string `json:"sourceId"`
	SourceName  string `json:"sourceName"`
	Grade       int    `json:"grade"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
	PageTotal   int    `json:"pageTotal"`
	PageCurrent int    `json:"pageCurrent"`
	SuccessCnt  int    `json:"successCnt"`
	FailedCnt   int    `json:"failedCnt"`
	FilmsTotal  int    `json:"filmsTotal"`
}

// FilmNotifyItem 影片明细项。
type FilmNotifyItem struct {
	Mid        int64  `json:"mid"`
	Name       string `json:"name"`
	SourceName string `json:"sourceName,omitempty"`
}

// NotifyTestResult 测试发送结果。
type NotifyTestResult struct {
	Sent   int               `json:"sent"`
	Failed []NotifyChatError `json:"failed,omitempty"`
}

// NotifyChatError 单个 Chat 发送失败。
type NotifyChatError struct {
	ChatID string `json:"chatId"`
	Error  string `json:"error"`
}

// NotifyChangeBatch 采集更新列表批次（MySQL，替代 Redis 会话，mid 无上限）。
type NotifyChangeBatch struct {
	ID        string    `gorm:"primaryKey;size:16"`
	SiteName  string    `gorm:"size:128"`
	PageSize  int       `gorm:"not null;default:15"`
	Total     int       `gorm:"not null;default:0"`
	Overview  string    `gorm:"type:text"` // 带按钮那一段概要，返回时 edit 用
	CreatedAt time.Time `gorm:"index"`
	ExpireAt  time.Time `gorm:"index"`
}

func (NotifyChangeBatch) TableName() string {
	return TableNotifyChangeBatch
}

// NotifyChangeMid 批次内变更影片 mid（全局去重）。
type NotifyChangeMid struct {
	BatchID    string `gorm:"primaryKey;size:16;index"`
	Mid        int64  `gorm:"primaryKey;index"`
	SourceName string `gorm:"size:1024"`
}

func (NotifyChangeMid) TableName() string {
	return TableNotifyChangeMid
}
