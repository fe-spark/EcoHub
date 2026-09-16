package spider

import (
	"fmt"
	"log"

	"server/internal/model"
	"server/internal/spider/progress"
	"server/internal/spider/scheduler"
)

// StopTask 停止单个站点：先置进度为已停止，再打断其写队列与运行中的任务上下文。
func StopTask(sourceID string) {
	progress.MarkStopped(sourceID)
	scheduler.CancelSource(model.SlaveCollect, sourceID)
	progress.CancelTask(sourceID)
}

// StopAllTasks 一键终止：先递增派发版本号阻断新站点启动，再停止全部活跃任务。
func StopAllTasks() {
	stopAllVersion.Add(1)
	progress.MarkAllRunningStopped()
	count := 0
	progress.RangeTasks(func(id, _ string) bool {
		count++
		progress.MarkStopped(id)
		scheduler.CancelSource(model.SlaveCollect, id)
		return true
	})
	progress.CancelAllTasks()
	if count > 0 {
		log.Printf("[Spider] 已强制停止 %d 个活跃采集任务\n", count)
	}
}

// PrepareSingleCollectStart 单站采集启动前校验重复并预置起始进度。
func PrepareSingleCollectStart(source model.FilmSource) error {
	if progress.IsAlreadyQueuedOrRunning(source.Id) {
		return fmt.Errorf("站点 %s 已在采集队列或正在运行，已跳过本次采集", source.Name)
	}
	progress.MarkSourcesCollectStarting([]model.FilmSource{source})
	return nil
}

// GetActiveTaskProgress 查询全部采集进度快照（含超时判定与终态清理）。
func GetActiveTaskProgress() []model.CollectProgress {
	return progress.GetActiveTaskProgress()
}

// IsTaskRunning 指定站点是否有活跃任务。
func IsTaskRunning(id string) bool {
	return progress.IsTaskRunning(id)
}

// IsAnyTaskRunning 是否存在任何活跃任务。
func IsAnyTaskRunning() bool {
	return progress.IsAnyTaskRunning()
}
