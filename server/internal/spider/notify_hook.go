package spider

import (
	"strings"
	"sync"
	"time"

	"server/internal/model"
	"server/internal/notify"
)

// sourceLastErrors 记录本批单源失败原因，供批次摘要填充 Error 行。
var sourceLastErrors sync.Map // sourceID -> string

func noteSourceError(sourceID, reason string) {
	sourceID = strings.TrimSpace(sourceID)
	reason = strings.TrimSpace(reason)
	if sourceID == "" || reason == "" {
		return
	}
	sourceLastErrors.Store(sourceID, reason)
}

func takeSourceError(sourceID string) string {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return ""
	}
	if v, ok := sourceLastErrors.LoadAndDelete(sourceID); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// emitBatchSummaryForSources 根据源列表与进度组装并发送批次摘要。
// 收尾失败只写入摘要的 FinalizeError，不再单独 PublishFinalizeFailed，避免双发。
func emitBatchSummaryForSources(batch *notify.ChangeBatch, trigger string, sources []model.FilmSource, startedAt time.Time, finalizeErr error) {
	if len(sources) == 0 {
		return
	}
	sourceIDs := make([]string, 0, len(sources))
	results := make([]model.SourceNotifyResult, 0, len(sources))
	for _, src := range sources {
		sourceIDs = append(sourceIDs, src.Id)
		progress, ok := collectProgressSnapshot(src.Id)
		if !ok {
			// 无进度时不默认 done，避免误报成功；由调用方保证有进度，或改用 Direct 结果。
			progress = model.CollectProgress{
				Id:     src.Id,
				Name:   src.Name,
				Status: progressStatusFailed,
			}
		}
		errMsg := takeSourceError(src.Id)
		if errMsg == "" && progress.Status == progressStatusFailed && finalizeErr != nil {
			errMsg = finalizeErr.Error()
		}
		results = append(results, notify.BuildSourceResult(src, progress, errMsg))
	}
	// 若收尾失败，把仍处于 finalizing 的源标为 failed
	if finalizeErr != nil {
		for i := range results {
			if results[i].Status == progressStatusFinalizing || results[i].Status == progressStatusWaitingPublish {
				results[i].Status = progressStatusFailed
				if results[i].Error == "" {
					results[i].Error = finalizeErr.Error()
				}
			}
		}
	}
	finMsg := ""
	if finalizeErr != nil {
		finMsg = finalizeErr.Error()
	}
	payload := notify.BuildBatchPayload(batch, trigger, results, startedAt, time.Now(), finMsg)
	// 摘要开启时收尾错误只写在摘要里；摘要关闭时才单独发 finalize 告警，避免双发。
	if finalizeErr != nil && !notify.IsEventEnabled(model.NotifyEventCollectBatchSummary) {
		notify.PublishFinalizeFailed(len(sources), finMsg)
	}
	notify.PublishBatchSummary(payload)
	// Drain 后兜底清 Acc / 错误缓存，降低跨批次串扰
	notify.Acc.ClearSources(sourceIDs...)
	for _, id := range sourceIDs {
		takeSourceError(id)
	}
}

// emitBatchSummaryDirect 使用调用方给出的源结果发摘要（不依赖 collectProgress）。
func emitBatchSummaryDirect(batch *notify.ChangeBatch, trigger string, results []model.SourceNotifyResult, startedAt time.Time, finalizeErr error) {
	if len(results) == 0 {
		return
	}
	sourceIDs := make([]string, 0, len(results))
	for _, r := range results {
		if id := strings.TrimSpace(r.SourceID); id != "" {
			sourceIDs = append(sourceIDs, id)
		}
	}
	finMsg := ""
	if finalizeErr != nil {
		finMsg = finalizeErr.Error()
	}
	payload := notify.BuildBatchPayload(batch, trigger, results, startedAt, time.Now(), finMsg)
	if finalizeErr != nil && !notify.IsEventEnabled(model.NotifyEventCollectBatchSummary) {
		notify.PublishFinalizeFailed(len(results), finMsg)
	}
	notify.PublishBatchSummary(payload)
	if len(sourceIDs) > 0 {
		notify.Acc.ClearSources(sourceIDs...)
		for _, id := range sourceIDs {
			takeSourceError(id)
		}
	}
}

// emitSourceFailedNotify 单源失败即时通知（限流在 notify 内）。
func emitSourceFailedNotify(sourceID, sourceName, reason string) {
	sourceID = strings.TrimSpace(sourceID)
	sourceName = strings.TrimSpace(sourceName)
	if sourceName == "" {
		sourceName = sourceID
	}
	noteSourceError(sourceID, reason)
	notify.PublishSourceFailed(sourceID, sourceName, reason)
}

// emitProgressStaleNotify 进度超时通知。
func emitProgressStaleNotify(sourceID, sourceName, oldStatus string, age time.Duration) {
	reason := "进度超时 status=" + oldStatus + " age=" + age.Round(time.Second).String()
	noteSourceError(sourceID, reason)
	notify.PublishProgressStale(sourceID, sourceName, oldStatus, age)
}

// noteCollectedMIDs 累计本源「应进更新列表」的 mid。
// 约定：列表条目必须是主站已有影片（全局 mid）；只有本源最大集数第一次超过全库最大才算更新：
//   - 主站：新片，或主站集数 > 全库（含附属站）已有最大集数；
//   - 附属站：已匹配主站 mid，且本源追集后集数 > 全库其它源；首次通过时 stamp=现在（不套主站旧时间）。
//
// 后到的源追到同一集数、仅链接刷新、备注/Hits 不计入；未匹配主站的附属片不进列表。
func noteCollectedMIDs(batch *notify.ChangeBatch, sourceID, sourceName string, mids []int64) {
	if len(mids) == 0 {
		return
	}
	notify.Acc.Add(batch, sourceID, sourceName, mids...)
}
