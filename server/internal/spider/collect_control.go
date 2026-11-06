package spider

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/model"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider/progress"
	"server/internal/spider/scheduler"
)

// deleteStopTimeout 删除前等待本次抓页退出的上限。
const deleteStopTimeout = 3 * time.Minute

// importStopTimeout 导入停采集的等待上限。只覆盖正在执行的写事务。
const importStopTimeout = 20 * time.Second

var (
	stopAllReason     atomic.Value
	sourceStopReasons sync.Map
)

func normalizeStopReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "未说明原因"
	}
	return reason
}

func currentStopAllReason() string {
	reason, _ := stopAllReason.Load().(string)
	if reason == "" {
		return "未说明原因"
	}
	return reason
}

func sourceStopReason(sourceID string) string {
	if v, ok := sourceStopReasons.Load(strings.TrimSpace(sourceID)); ok {
		if reason, ok := v.(string); ok {
			return reason
		}
	}
	return ""
}

func rememberSourceStop(sourceID, reason string) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	sourceStopReasons.Store(sourceID, reason)
}

// collectStopReason 站点这次被停掉的原因。单站原因优先于最近一次全部停止。
func collectStopReason(sourceID string) string {
	if reason := sourceStopReason(sourceID); reason != "" {
		return reason
	}
	return currentStopAllReason()
}

// StopAllCollect 停止全部采集，并记下原因供后续派发日志使用。
func StopAllCollect(reason string) {
	reason = normalizeStopReason(reason)
	stopAllReason.Store(reason)
	stopAllVersion.Add(1)
	ids := make([]string, 0)
	progress.RangeTasks(func(id, _ string) bool {
		ids = append(ids, id)
		return true
	})
	for _, id := range ids {
		rememberSourceStop(id, reason)
		log.Printf("[Spider] 停止站点采集：id=%s，%s", id, reason)
		progress.RequestStop(id)
	}
	progress.MarkAllRunningStopped()
	releaseQueuedCollectOccupies()
	scheduler.CancelPendingWrites()
	log.Printf("[Spider] 停止全部采集：%s，活跃任务 %d 个", reason, len(ids))
}

// StopTask 用户停止单个采集站。
func StopTask(sourceID string) {
	StopCollectSource(sourceID, "用户停止该站")
}

// StopCollectSource 停止单个采集站，日志写明原因。
func StopCollectSource(sourceID, reason string) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	reason = normalizeStopReason(reason)
	rememberSourceStop(sourceID, reason)
	log.Printf("[Spider] 停止站点采集：id=%s，%s", sourceID, reason)
	progress.RequestStop(sourceID)
	releaseCollectSourceIDs(sourceID)
	scheduler.CancelSource(sourceID)
}

// StopSourceForDelete 占住该站。采集中（含待检索）的删除已在入队前拒绝。
// 调用方删除结束后必须调用 release，否则这个站无法再开采集。
func StopSourceForDelete(sourceID string) (release func(), err error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return func() {}, nil
	}
	if err := collectLifecycle.beginSource(sourceID); err != nil {
		log.Printf("[Spider] 删除站点占位失败 id=%s err=%v", sourceID, err)
		return nil, err
	}
	log.Printf("[Spider] 删除站点已占位，开始清数据 id=%s", sourceID)
	return func() { collectLifecycle.endSource(sourceID) }, nil
}

// SearchWorkBusyReason 检索索引或封口还在进行时返回原因。空字符串表示空闲。
func SearchWorkBusyReason() string {
	parts := make([]string, 0, 4)
	if filmsnapshot.IsSearchIndexBuilding() {
		parts = append(parts, "索引构建中")
	}
	if filmsnapshot.SearchRebuildScheduled() {
		parts = append(parts, "重建已排队")
	}
	if searchSealRunning.Load() {
		parts = append(parts, "封口执行中")
	}
	searchReseal.mu.Lock()
	running := searchReseal.running
	pending := len(searchReseal.pending)
	sealing := len(searchReseal.sealing)
	searchReseal.mu.Unlock()
	if running {
		parts = append(parts, fmt.Sprintf("封口队列运行中 pending=%d sealing=%d", pending, sealing))
	}
	return strings.Join(parts, "，")
}

// StopAllTasks 用户一键终止全部采集。
func StopAllTasks() {
	StopAllCollect("用户一键终止")
}

// StopAllTasksAndWait 按给定原因停止全部采集，并等待正在写的事务结束。
func StopAllTasksAndWait(timeout time.Duration, reason string) error {
	if timeout <= 0 {
		timeout = importStopTimeout
	}
	StopAllCollect(reason)
	return collectLifecycle.waitNotBusy(timeout)
}

// PrepareSingleCollectStart 单站采集启动前占用采集源并预置起始进度。
func PrepareSingleCollectStart(source model.FilmSource) error {
	claimed, err := occupyAndMarkCollectSources([]model.FilmSource{source}, "Single-Collect")
	if err != nil {
		return err
	}
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
