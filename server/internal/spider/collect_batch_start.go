package spider

import (
	"strings"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider/progress"
)

// PrepareBatchCollectStart 挑出这次能开的站并占用。跳过的站带原因返回，不占队列。
// 定时采集不走这里，仍只跳过并写日志。
func PrepareBatchCollectStart(ids []string) ([]model.FilmSource, model.CollectBatchResult) {
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

	claimed := occupyAndMarkCollectSources(ready, "Batch-Collect")
	claimedIDs := make(map[string]struct{}, len(claimed))
	started := make([]model.CollectBatchItem, 0, len(claimed))
	for _, source := range claimed {
		claimedIDs[source.Id] = struct{}{}
		started = append(started, model.CollectBatchItem{Id: source.Id, Name: source.Name})
	}
	for _, source := range ready {
		if _, ok := claimedIDs[source.Id]; ok {
			continue
		}
		skipped = append(skipped, model.CollectBatchSkip{Id: source.Id, Name: source.Name, Reason: "采集中"})
	}
	return claimed, model.CollectBatchResult{Started: started, Skipped: skipped}
}

func batchCollectBlockReason(fs model.FilmSource) string {
	if progress.IsCategorySyncing(fs.Id) {
		return "分类采集中"
	}
	if progress.IsStopping(fs.Id) {
		return "停止中"
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
