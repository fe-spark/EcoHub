package spider

import (
	"strings"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider/progress"
)

// PrepareBatchCollectStart 挑出这次能开的站并占用。跳过的站带原因返回，不占队列。
// 手动批量采集与计划任务定时采集均复用此逻辑。
func PrepareBatchCollectStart(ids []string) (CollectClaim, model.CollectBatchResult) {
	if reason := collectPauseReason(); reason != "" {
		skipped := make([]model.CollectBatchSkip, 0, len(ids))
		for _, raw := range ids {
			id := strings.TrimSpace(raw)
			if id == "" {
				continue
			}
			skipped = append(skipped, model.CollectBatchSkip{Id: id, Reason: reason})
		}
		return CollectClaim{}, model.CollectBatchResult{Skipped: skipped}
	}
	seen := make(map[string]struct{}, len(ids))
	ready := make([]model.FilmSource, 0, len(ids))
	skipped := make([]model.CollectBatchSkip, 0)
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		fs := repository.FindCollectSourceById(id)
		if fs == nil {
			skipped = append(skipped, model.CollectBatchSkip{Id: id, Reason: "采集站不存在"})
			continue
		}
		if reason := batchCollectBlockReason(*fs); reason != "" {
			skipped = append(skipped, model.CollectBatchSkip{Id: fs.Id, Name: fs.Name, Reason: reason})
			continue
		}
		ready = append(ready, *fs)
	}

	claim, err := claimCollectSources(ready, "批量采集")
	if err != nil {
		for _, source := range ready {
			skipped = append(skipped, model.CollectBatchSkip{Id: source.Id, Name: source.Name, Reason: err.Error()})
		}
		return CollectClaim{}, model.CollectBatchResult{Skipped: skipped}
	}
	claimedIDs := make(map[string]struct{}, len(claim.Sources))
	started := make([]model.CollectBatchItem, 0, len(claim.Sources))
	for _, source := range claim.Sources {
		claimedIDs[source.Id] = struct{}{}
		started = append(started, model.CollectBatchItem{Id: source.Id, Name: source.Name})
	}
	for _, source := range ready {
		if _, ok := claimedIDs[source.Id]; ok {
			continue
		}
		skipped = append(skipped, model.CollectBatchSkip{Id: source.Id, Name: source.Name, Reason: "采集中"})
	}
	// 名额和「已开始」一起交出去。异步开工后再被封口拒绝，调用方已经拿到了开始名单。
	if len(claim.Sources) > 0 {
		if beforeCollectQueueAdmit != nil {
			beforeCollectQueueAdmit()
		}
		if _, err := tryBeginCollectQueue(); err != nil {
			for _, source := range claim.Sources {
				skipped = append(skipped, model.CollectBatchSkip{Id: source.Id, Name: source.Name, Reason: err.Error()})
			}
			claim.abandon()
			return CollectClaim{}, model.CollectBatchResult{Skipped: skipped}
		}
		claim.queueHeld = true
	}
	return claim, model.CollectBatchResult{Started: started, Skipped: skipped}
}

// beforeCollectQueueAdmit 仅供测试：占用已经成功、队列名额还没占下时调用。
var beforeCollectQueueAdmit func()

func batchCollectBlockReason(fs model.FilmSource) string {
	if isMaintenanceRunning() {
		return "维护任务执行中（" + maintenanceRunningDetail() + "），已跳过"
	}
	if reason := collectPauseReason(); reason != "" {
		return reason
	}
	if progress.IsCategorySyncing(fs.Id) {
		return "分类采集中"
	}
	if progress.IsStopping(fs.Id) {
		return "停止中"
	}
	if progress.IsSealing(fs.Id) {
		return "待检索"
	}
	if progress.IsAlreadyQueuedOrRunning(fs.Id) {
		return "采集中"
	}
	parts := make([]string, 0, 2)
	if !fs.State {
		parts = append(parts, "未启用")
	}
	if !repository.SourceHasCategoryMapping(fs.Id) {
		parts = append(parts, "无分类")
	}
	return strings.Join(parts, "、")
}
