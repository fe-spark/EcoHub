package spider

import (
	"fmt"
	"log"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/spider/progress"
	"server/internal/spider/scheduler"
)

// deleteStopTimeout 删除前等待本次采集退出的上限。
const deleteStopTimeout = 3 * time.Minute

// StopTask 停止单个站点：打断写队列，并立刻从采集队列摘掉。
// 已经跑起来的站进度保持「停止中」，等本次运行真正退出后再标成已停止。
// 还没跑起来的站直接标成已停止。两种情况都马上放掉占用，新采集不会被旧批次盖掉。
func StopTask(sourceID string) {
	scheduler.CancelSource(sourceID)
	progress.RequestStop(sourceID)
	releaseCollectSourceIDs(sourceID)
}

// StopSourceForDelete 先停止该站并等到本次运行退出，再占住生命周期。
// 调用方删除结束后必须调用 release，否则这个站无法再开采集。
func StopSourceForDelete(sourceID string) (release func(), err error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return func() {}, nil
	}
	if err := stopAndWaitSource(sourceID); err != nil {
		return nil, err
	}
	if err := collectLifecycle.beginSource(sourceID); err != nil {
		if err := stopAndWaitSource(sourceID); err != nil {
			return nil, err
		}
		if err := collectLifecycle.beginSource(sourceID); err != nil {
			return nil, err
		}
	}
	return func() { collectLifecycle.endSource(sourceID) }, nil
}

func stopAndWaitSource(sourceID string) error {
	StopTask(sourceID)
	return collectLifecycle.waitSource(sourceID, deleteStopTimeout)
}

// StopAllTasks 一键终止：先递增派发版本号阻断新站点启动，再停止全部活跃任务。
func StopAllTasks() {
	stopAllVersion.Add(1)
	ids := make([]string, 0)
	progress.RangeTasks(func(id, _ string) bool {
		ids = append(ids, id)
		return true
	})
	for _, id := range ids {
		scheduler.CancelSource(id)
		progress.RequestStop(id)
	}
	progress.MarkAllRunningStopped()
	releaseQueuedCollectOccupies()
	if len(ids) > 0 {
		log.Printf("[Spider] 已强制停止 %d 个活跃采集任务\n", len(ids))
	}
}

// PrepareSingleCollectStart 单站采集启动前占用采集源并预置起始进度。
func PrepareSingleCollectStart(source model.FilmSource) error {
	claimed := occupyAndMarkCollectSources([]model.FilmSource{source}, "Single-Collect")
	if len(claimed) == 0 {
		return fmt.Errorf("站点 %s 已在采集队列或正在运行，已跳过本次采集", source.Name)
	}
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
