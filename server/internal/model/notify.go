package model

import "time"

const (
	// DefaultNotifyPageSize 翻页每页默认展示影片条数
	DefaultNotifyPageSize = 15
	// MaxNotifyPageSize 翻页每页最大展示影片条数
	MaxNotifyPageSize = 20
)

// NotifyEventSwitches Telegram 通知事件开关（聚焦采集汇总与核心失败告警）
type NotifyEventSwitches struct {
	CollectBatchSummary bool `json:"collectBatchSummary"` // 采集批次完成摘要（含更新片单）
	CollectSourceFailed bool `json:"collectSourceFailed"` // 单源连续失败终止告警
	CronTaskFailed      bool `json:"cronTaskFailed"`      // 定时任务执行失败告警
	SourceOrderChange   bool `json:"sourceOrderChange"`   // 采集站排序与首选站切换通知
}

// NotifyConfig Telegram 通知配置（持久化于 MySQL + Redis 缓存）
type NotifyConfig struct {
	Enabled            bool                `json:"enabled"`
	BotToken           string              `json:"botToken"`
	ChatIDs            []string            `json:"chatIds"` // 支持 "chatId" 或 "chatId:threadId"
	Events             NotifyEventSwitches `json:"events"`
	OnlyNotifyOnUpdate bool                `json:"onlyNotifyOnUpdate"` // 无更新且无失败时跳过汇总推送
	MaxFilmsInMessage  int                 `json:"maxFilmsInMessage"`  // 翻页列表每页展示条数（1~20，默认 15）
}

// NotifyConfigRecord 通知配置持久化模型（单行 JSON 表）
type NotifyConfigRecord struct {
	ID        uint `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	Payload   string `gorm:"type:text;not null"`
}

// TableName 返回通知配置物理表名
func (NotifyConfigRecord) TableName() string {
	return TableNotifyConfig
}

// SourceNotifyResult 单个采集源在批次中的执行统计
type SourceNotifyResult struct {
	SourceID    string `json:"sourceId"`
	SourceName  string `json:"sourceName"`
	Sort        int    `json:"sort"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
	PageTotal   int    `json:"pageTotal"`
	PageCurrent int    `json:"pageCurrent"`
	SuccessCnt  int    `json:"successCnt"`
	FailedCnt   int    `json:"failedCnt"`
	FilmsTotal  int    `json:"filmsTotal"`
}

// CollectBatchNotifyPayload 采集批次汇总通知载荷
type CollectBatchNotifyPayload struct {
	Trigger    string               `json:"trigger"`
	StartedAt  time.Time            `json:"startedAt"`
	FinishedAt time.Time            `json:"finishedAt"`
	Sources    []SourceNotifyResult `json:"sources"`
	NotifyMIDs []int64              `json:"notifyMids"`
}

// NotifyFilmItem 通知翻页会话中的单片精简项
type NotifyFilmItem struct {
	Mid  int64  `json:"mid"`
	Name string `json:"name"`
}

// NotifyCategoryGroup 通知翻页会话中的分类聚合项
type NotifyCategoryGroup struct {
	CategoryID   int64            `json:"categoryId"`
	CategoryName string           `json:"categoryName"`
	Films        []NotifyFilmItem `json:"films"`
}

// NotifyBatchSession TG 消息内联按钮翻页会话（存于内存 + Redis，TTL 24h）
type NotifyBatchSession struct {
	BatchID      string                `json:"batchId"`
	SiteName     string                `json:"siteName"`
	OverviewHTML string                `json:"overviewHtml"`
	PageSize     int                   `json:"pageSize"`
	Categories   []NotifyCategoryGroup `json:"categories"`
	AllFilms     []NotifyFilmItem      `json:"allFilms"`
}

// NotifyTestReq 测试发送通知请求参数（支持草稿）
type NotifyTestReq struct {
	BotToken string   `json:"botToken"`
	ChatIDs  []string `json:"chatIds"`
}

// NotifyChatError 单个 Chat 发送失败明细
type NotifyChatError struct {
	ChatID string `json:"chatId"`
	Error  string `json:"error"`
}

// NotifyTestResult 测试发送通知响应结果
type NotifyTestResult struct {
	Sent   int               `json:"sent"`
	Failed []NotifyChatError `json:"failed,omitempty"`
}
