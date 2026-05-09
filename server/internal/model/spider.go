package model

import (
	"time"

	"server/internal/model/dto"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// FilmCollectTask 影视采集任务
type FilmCollectTask struct {
	Id     string       `json:"id"`     // 唯一标识uid
	Ids    []string     `json:"ids"`    // 采集站id列表
	Cid    cron.EntryID `json:"cid"`    // 定时任务Id (运行时字段，不持久化)
	Time   int          `json:"time"`   // 采集时长, 最新x小时更新的内容
	Spec   string       `json:"spec"`   // 执行周期 cron表达式
	Model  int          `json:"model"`  // 任务类型, 0 - 自动更新已启用主站 || 1 - 更新Ids中的主站数据 || 2 - 定期清理失败采集记录
	State  bool         `json:"state"`  // 状态 开启 | 禁用
	Remark string       `json:"remark"` // 任务备注信息
}

// CrontabRecord 定时任务持久化模型 (MySQL)
type CrontabRecord struct {
	gorm.Model
	TaskId    string `gorm:"uniqueIndex;size:64"`
	Time      int
	Spec      string `gorm:"size:64"`
	TaskModel int    `gorm:"column:task_model"` // 任务类型 (避免与 gorm.Model 嵌入名冲突)
	State     bool
	Remark    string `gorm:"size:256"`
}

// CronSourceRel 定时任务与资源站关联表
type CronSourceRel struct {
	TaskId   string `gorm:"primaryKey;index;size:64"`
	SourceId string `gorm:"primaryKey;index;size:64"`
}

func (CronSourceRel) TableName() string {
	return TableCronSourceRel
}

type SourceGrade int

const (
	MasterCollect SourceGrade = iota
	SlaveCollect
)

// FilmSource 影视站点信息保存结构体
type FilmSource struct {
	Id           string      `json:"id" gorm:"primaryKey;size:32"`    // 唯一ID
	Name         string      `json:"name" gorm:"size:64"`             // 采集站点备注名
	Uri          string      `json:"uri" gorm:"uniqueIndex;size:255"` // 采集链接
	Grade        SourceGrade `json:"grade"`                           // 采集站等级 主站点 || 附属站
	SyncPictures bool        `json:"syncPictures"`                    // 是否同步图片到服务器
	State        bool        `json:"state"`                           // 是否启用
	Interval     int         `json:"interval"`                        // 采集时间间隔 单位/ms
}

func (f *FilmSource) TableName() string {
	return "film_sources"
}

type CollectSourceStats struct {
	gorm.Model
	SourceId        string     `gorm:"uniqueIndex;size:32"`
	LastCollectTime *time.Time `gorm:"index"`
}

func (CollectSourceStats) TableName() string {
	return TableCollectSourceStats
}

// FailureRecord 失败采集记录信息机构体
type FailureRecord struct {
	gorm.Model
	OriginId   string `json:"originId"`   // 采集站唯一ID
	OriginName string `json:"originName"` // 采集站唯一ID
	Uri        string `json:"uri"`        // 采集源链接
	PageNumber int    `json:"pageNumber"` // 页码
	Hour       int    `json:"hour"`       // 采集参数 h 时长
	Cause      string `json:"cause"`      // 失败原因
	Status     int    `json:"status"`     // 重试状态
	RetryCount int    `json:"retryCount"` // 重试累计次数
}

const (
	// FailureRecordStatusPending 等待后续定时重试。
	FailureRecordStatusPending = 1
	// FailureRecordStatusSuccess 本次重试已成功，不再进入后续定时队列。
	FailureRecordStatusSuccess = 0
	// FailureRecordStatusFailed 已达到最大自动重试次数，不再进入后续定时队列。
	FailureRecordStatusFailed = 2
)

func (fr FailureRecord) TableName() string {
	return "failure_records"
}

type RecordRequestVo struct {
	OriginId  string    `json:"originId"`  // 源站点ID
	Hour      int       `json:"hour"`      // 采集时长
	Status    int       `json:"status"`    // 状态
	BeginTime time.Time `json:"beginTime"` // 起始时间
	EndTime   time.Time `json:"endTime"`   // 结束时间
	Paging    *dto.Page `json:"paging"`    // 分页参数
}

// CronTaskVo 定时任务数据response
type CronTaskVo struct {
	FilmCollectTask
	PreV    string `json:"preV"`    // 上次执行时间
	Next    string `json:"next"`    // 下次执行时间
	Running bool   `json:"running"` // 当前是否正在执行
}

// FilmTaskOptions 影视采集任务添加时需要的options
type FilmTaskOptions struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

type FilmSourceStateBatchRequest struct {
	Ids   []string `json:"ids"`
	State bool     `json:"state"`
}

type CollectProgress struct {
	Id      string `json:"id"`
	Name    string `json:"name"`
	Total   int    `json:"total"`
	Current int    `json:"current"`
	Success int    `json:"success"`
	Failed  int    `json:"failed"`
	Status  string `json:"status"`
}

type FilmSourceListItem struct {
	FilmSource
	LastCollectTime *time.Time       `json:"lastCollectTime,omitempty"`
	Progress        *CollectProgress `json:"progress,omitempty"`
}

type Option struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

type OptionGroup map[string][]Option

// CollectParams 数据采集所需要的参数
type CollectParams struct {
	Id    string   `json:"id"`    // 资源站id
	Ids   []string `json:"ids"`   // 资源站id列表
	Time  int      `json:"time"`  // 采集时长
	Batch bool     `json:"batch"` // 是否批量执行
}
