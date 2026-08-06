package model

import "time"

// NotifyConfig Telegram 通知配置（MySQL JSON + Redis 缓存）。
type NotifyConfig struct {
	Enabled  bool     `json:"enabled"`
	BotToken string   `json:"botToken"`
	ChatIDs  []string `json:"chatIds"`

	Events NotifyEventSwitches `json:"events"`

	IncludeFilmDetails bool `json:"includeFilmDetails"`
	MaxFilmsInMessage  int  `json:"maxFilmsInMessage"`
	MinIntervalSec     int  `json:"minIntervalSec"`
}

// NotifyEventSwitches 各事件开关。
type NotifyEventSwitches struct {
	CollectBatchSummary   bool `json:"collectBatchSummary"`
	CollectSourceFailed   bool `json:"collectSourceFailed"`
	CollectFinalizeFailed bool `json:"collectFinalizeFailed"`
	CollectProgressStale  bool `json:"collectProgressStale"`
	CronTaskFailed        bool `json:"cronTaskFailed"`
	CronTaskDone          bool `json:"cronTaskDone"`
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
}

// SourceNotifyResult 单源结果。
type SourceNotifyResult struct {
	SourceID    string           `json:"sourceId"`
	SourceName  string           `json:"sourceName"`
	Grade       int              `json:"grade"`
	Status      string           `json:"status"`
	Error       string           `json:"error,omitempty"`
	PageTotal   int              `json:"pageTotal"`
	PageCurrent int              `json:"pageCurrent"`
	SuccessCnt  int              `json:"successCnt"`
	FailedCnt   int              `json:"failedCnt"`
	Films       []FilmNotifyItem `json:"films,omitempty"`
	FilmsTotal  int              `json:"filmsTotal"`
	FilmsTrunc  bool             `json:"filmsTruncated"`
}

// FilmNotifyItem 影片明细项。
type FilmNotifyItem struct {
	Mid  int64  `json:"mid"`
	Name string `json:"name"`
}

// NotifyTestResult 测试发送结果。
type NotifyTestResult struct {
	Sent   int                `json:"sent"`
	Failed []NotifyChatError  `json:"failed,omitempty"`
}

// NotifyChatError 单个 Chat 发送失败。
type NotifyChatError struct {
	ChatID string `json:"chatId"`
	Error  string `json:"error"`
}
