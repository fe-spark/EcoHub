package model

import (
	"time"
)

const (
	CollectRecordStatusRunning        = "running"
	CollectRecordStatusSuccess        = "success"
	CollectRecordStatusPartial        = "partial"
	CollectRecordStatusPartialStopped = "partial_stopped"
	CollectRecordStatusFailed         = "failed"
	CollectRecordStatusStopped        = "stopped"
	CollectRecordStatusInterrupted    = "interrupted"
)

// CollectBatchStatus 由各站终态和页数得到批次状态。有站点异常中断时，整批就是中断。
func CollectBatchStatus(done, partial, failed, stopped, interrupted, total, succPages, failPages int) string {
	if interrupted > 0 {
		return CollectRecordStatusInterrupted
	}
	if total <= 0 || (failed == 0 && partial == 0 && stopped == 0 && done == total && failPages == 0) {
		return CollectRecordStatusSuccess
	}
	if failed == 0 && partial == 0 && stopped > 0 {
		if done > 0 {
			return CollectRecordStatusPartialStopped
		}
		return CollectRecordStatusStopped
	}
	if failPages > 0 || failed > 0 || partial > 0 {
		if succPages > 0 || done > 0 || partial > 0 {
			return CollectRecordStatusPartial
		}
		return CollectRecordStatusFailed
	}
	if done == total {
		return CollectRecordStatusSuccess
	}
	return CollectRecordStatusFailed
}

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
	Trigger        string              `gorm:"index;size:32;not null" json:"trigger"`       // manual / cron / retry
	Tag            string              `gorm:"size:64" json:"tag"`                          // 单站采集 / 批量采集 / 失败重试 / 定时采集等
	Hour           int                 `gorm:"not null;default:0" json:"hour"`              // 采集跨度：-1全量，>0小时
	Status         string              `gorm:"index;size:32;not null" json:"status"`        // running / success / partial / failed / stopped / interrupted
	SourceCount    int                 `gorm:"not null;default:0" json:"sourceCount"`       // 覆盖站点数
	SuccessSources int                 `gorm:"not null;default:0" json:"successSources"`    // 成功站点数
	FailedSources  int                 `gorm:"not null;default:0" json:"failedSources"`     // 失败站点数
	FilmCount      int                 `gorm:"not null;default:0" json:"filmCount"`         // 更新影片部数（内存聚合统计）
	TargetMids     string              `gorm:"type:text" json:"-"`                          // 仅单片更新记录目标影片 ID 的 JSON 数组
	FilmName       string              `gorm:"-" json:"filmName,omitempty"`                 // 单片更新的片名，按 TargetMids 从影片库回填
	Films          []CollectRecordFilm `gorm:"-" json:"films,omitempty"`                    // 单片更新可跳转的影片
	PageTotal      int                 `gorm:"not null;default:0" json:"pageTotal"`         // 总页数
	PageSuccess    int                 `gorm:"not null;default:0" json:"pageSuccess"`       // 成功页数
	PageFailed     int                 `gorm:"not null;default:0" json:"pageFailed"`        // 失败页数
	DurationMs     int64               `gorm:"not null;default:0" json:"durationMs"`        // 耗时毫秒
	StartedAt      time.Time           `gorm:"index;not null" json:"startedAt"`             // 开始时间
	FinishedAt     *time.Time          `json:"finishedAt,omitempty"`                        // 完成时间
	ErrorMsg       string              `gorm:"type:text" json:"errorMsg,omitempty"`         // 错误摘要
	DetailJson     string              `gorm:"type:mediumtext" json:"detailJson,omitempty"` // 站点明细 JSON，含全部失败页，TEXT 64KB 装不下大站的失败页
}

// CollectRecordFilm 单片更新回显的影片项。
type CollectRecordFilm struct {
	Mid      int64  `json:"mid"`
	Name     string `json:"name"`
	SourceID string `json:"sourceId,omitempty"`
}

func (CollectRecord) TableName() string {
	return TableCollectRecord
}

// CollectRecordSourceDetail 批次中单站的执行明细快照
type CollectRecordSourceDetail struct {
	SourceID    string `json:"sourceId"`
	SourceName  string `json:"sourceName"`
	Status      string `json:"status"`         // success / failed / stopped
	Hour        int    `json:"hour,omitempty"` // 该站实际拉取的小时窗口，-1 为全量
	PageTotal   int    `json:"pageTotal"`
	PageCurrent int    `json:"pageCurrent"`
	SuccessCnt  int    `json:"successCnt"`
	FailedCnt   int    `json:"failedCnt"`
	FilmsTotal  int    `json:"filmsTotal"`
	Error       string `json:"error,omitempty"`
	FailedPages []int  `json:"failedPages,omitempty"`
}

// CollectRecordCompletion 批次执行结束结算载荷
type CollectRecordCompletion struct {
	Status         string    `json:"status"`
	Hour           int       `json:"hour"`
	SourceCount    int       `json:"sourceCount,omitempty"`
	SuccessSources int       `json:"successSources"`
	FailedSources  int       `json:"failedSources"`
	FinishedAt     time.Time `json:"finishedAt"`
	DurationMs     int64     `json:"durationMs"`
	FilmCount      int       `json:"filmCount"`
	PageTotal      int       `json:"pageTotal"`
	PageSuccess    int       `json:"pageSuccess"`
	PageFailed     int       `json:"pageFailed"`
	ErrorMsg       string    `json:"errorMsg"`
	DetailJson     string    `json:"detailJson"`
	TargetMids     string    `json:"targetMids,omitempty"`
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
	BatchID     string    `json:"batchId"`
}
