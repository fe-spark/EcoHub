package model

import (
	"time"
)

const (
	CollectRecordStatusRunning     = "running"
	CollectRecordStatusSuccess     = "success"
	CollectRecordStatusPartial     = "partial"
	CollectRecordStatusFailed      = "failed"
	CollectRecordStatusStopped     = "stopped"
	CollectRecordStatusInterrupted = "interrupted"
)

// 批次 Tag 常量
const (
	CollectTagBatchCollect  = "batch_collect"  // 批量采集
	CollectTagSingleCollect = "single_collect" // 单站采集
	CollectTagRetryCollect  = "retry_collect"  // 失败重试
	CollectTagCronCollect   = "cron_collect"   // 定时任务
	CollectTagSingleUpdate  = "single_update"  // 单片更新
)

// CollectRecord 采集批次运行历史记录
type CollectRecord struct {
	ID             uint                `gorm:"primaryKey;autoIncrement" json:"id"`
	BatchID        string              `gorm:"uniqueIndex;size:64;not null" json:"batchId"`
	ParentBatchID  string              `gorm:"index;size:64" json:"parentBatchId,omitempty"`
	Trigger        string              `gorm:"index;size:32;not null" json:"trigger"`    // manual / cron / retry
	Tag            string              `gorm:"size:64" json:"tag"`                       // 单站采集 / 批量采集 / 失败重试 / 定时采集等
	Hour           int                 `gorm:"not null;default:0" json:"hour"`           // 采集跨度：-1全量，>0小时
	Status         string              `gorm:"index;size:32;not null" json:"status"`     // running / success / partial / failed / stopped / interrupted
	SourceCount    int                 `gorm:"not null;default:0" json:"sourceCount"`    // 覆盖站点数
	SuccessSources int                 `gorm:"not null;default:0" json:"successSources"` // 成功站点数
	FailedSources  int                 `gorm:"not null;default:0" json:"failedSources"`  // 失败站点数
	FilmCount      int                 `gorm:"not null;default:0" json:"filmCount"`      // 影响影片数
	FilmIDs        string              `gorm:"size:1024" json:"filmIds,omitempty"`       // 单片更新要重放的全局影片编号
	FilmName       string              `gorm:"-" json:"filmName,omitempty"`              // 单片更新的片名，按 filmIds 从影片库回填
	Films          []CollectRecordFilm `gorm:"-" json:"films,omitempty"`                 // 单片更新可跳转的影片
	PageTotal      int                 `gorm:"not null;default:0" json:"pageTotal"`      // 总页数
	PageSuccess    int                 `gorm:"not null;default:0" json:"pageSuccess"`    // 成功页数
	PageFailed     int                 `gorm:"not null;default:0" json:"pageFailed"`     // 失败页数
	DurationMs     int64               `gorm:"not null;default:0" json:"durationMs"`     // 耗时毫秒
	StartedAt      time.Time           `gorm:"index;not null" json:"startedAt"`          // 开始时间
	FinishedAt     *time.Time          `json:"finishedAt,omitempty"`                     // 完成时间
	ErrorMsg       string              `gorm:"type:text" json:"errorMsg,omitempty"`      // 错误摘要
	DetailJson     string              `gorm:"type:text" json:"detailJson,omitempty"`    // 站点明细 JSON
}

// CollectRecordFilm 单片更新记录上可定位的影片。
type CollectRecordFilm struct {
	Mid  int64  `json:"mid"`
	Name string `json:"name"`
}

func (CollectRecord) TableName() string {
	return TableCollectRecord
}

// CollectRecordSourceDetail 批次中单站的执行明细快照
type CollectRecordSourceDetail struct {
	SourceID    string              `json:"sourceId"`
	SourceName  string              `json:"sourceName"`
	Status      string              `json:"status"` // success / failed / stopped
	PageTotal   int                 `json:"pageTotal"`
	PageCurrent int                 `json:"pageCurrent"`
	SuccessCnt  int                 `json:"successCnt"`
	FailedCnt   int                 `json:"failedCnt"`
	FilmsTotal  int                 `json:"filmsTotal"`
	Films       []CollectRecordFilm `json:"films,omitempty"` // 该站本次入库/更新的影片快照
	Error       string              `json:"error,omitempty"`
}

// CollectRecordCompletion 批次执行结束结算载荷
type CollectRecordCompletion struct {
	Status         string    `json:"status"`
	Hour           int       `json:"hour"`
	SuccessSources int       `json:"successSources"`
	FailedSources  int       `json:"failedSources"`
	FinishedAt     time.Time `json:"finishedAt"`
	DurationMs     int64     `json:"durationMs"`
	FilmCount      int       `json:"filmCount"`
	FilmIDs        string    `json:"filmIds,omitempty"`
	PageTotal      int       `json:"pageTotal"`
	PageSuccess    int       `json:"pageSuccess"`
	PageFailed     int       `json:"pageFailed"`
	ErrorMsg       string    `json:"errorMsg"`
	DetailJson     string    `json:"detailJson"`
}

// CollectRecordProgressPayload 批次采集实时进度与终态载荷（供 SSE 流式推送）
type CollectRecordProgressPayload struct {
	BatchID        string     `json:"batchId"`
	Status         string     `json:"status"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty"`
	DurationMs     int64      `json:"durationMs"`
	FilmCount      int        `json:"filmCount"`
	PageTotal      int        `json:"pageTotal"`
	PageSuccess    int        `json:"pageSuccess"`
	PageFailed     int        `json:"pageFailed"`
	SuccessSources int        `json:"successSources"`
	FailedSources  int        `json:"failedSources"`
	ErrorMsg       string     `json:"errorMsg,omitempty"`
	DetailJson     string     `json:"detailJson,omitempty"`
}

// CollectRecordRetryReq 重试失败站点请求参数
type CollectRecordRetryReq struct {
	BatchID   string   `json:"batchId"`
	SourceIDs []string `json:"sourceIds,omitempty"`
}

// SourceIncident 同一采集源当前未自愈的失败折叠。
type SourceIncident struct {
	SourceID            string    `json:"sourceId"`
	SourceName          string    `json:"sourceName"`
	ConsecutiveFailures int       `json:"consecutiveFailures"`
	LatestFailedAt      time.Time `json:"latestFailedAt"`
	LatestError         string    `json:"latestError,omitempty"`
	LatestBatchID       string    `json:"latestBatchId"`
	Tripped             bool      `json:"tripped"`
}

// CollectRecordQuery 采集记录查询入参
type CollectRecordQuery struct {
	Page        int       `json:"page"`
	PageSize    int       `json:"pageSize"`
	Trigger     string    `json:"trigger"`
	Status      string    `json:"status"`
	BeginTime   time.Time `json:"beginTime"`
	EndTime     time.Time `json:"endTime"`
	UpdatedOnly bool      `json:"updatedOnly"` // 只看有入库
	FailedOnly  bool      `json:"failedOnly"`  // 只看有失败或中断
}
