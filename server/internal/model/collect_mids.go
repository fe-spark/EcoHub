package model

// 采集任务终态常量（跨模块共享，避免通知/统计域反向依赖爬虫进度包）。
const (
	CollectStatusDone    = "done"
	CollectStatusFailed  = "failed"
	CollectStatusStopped = "stopped"
)

// Mids 一页写入产生的 mid 集合：Notify 用于变更通知，Affected 用于快照/缓存收尾。
type Mids struct {
	Notify   []int64
	Affected []int64
	Unmapped int // 分类未映射，未入库
}
