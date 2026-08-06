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
func emitBatchSummaryForSources(trigger string, sources []model.FilmSource, startedAt time.Time, finalizeErr error) {
	if len(sources) == 0 {
		return
	}
	results := make([]model.SourceNotifyResult, 0, len(sources))
	for _, src := range sources {
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
		notify.PublishFinalizeFailed(len(sources), finMsg)
	}
	payload := notify.BuildBatchPayload(trigger, results, startedAt, time.Now(), finMsg)
	notify.PublishBatchSummary(payload)
}

// emitBatchSummaryDirect 使用调用方给出的源结果发摘要（不依赖 collectProgress）。
func emitBatchSummaryDirect(trigger string, results []model.SourceNotifyResult, startedAt time.Time, finalizeErr error) {
	if len(results) == 0 {
		return
	}
	finMsg := ""
	if finalizeErr != nil {
		finMsg = finalizeErr.Error()
		notify.PublishFinalizeFailed(len(results), finMsg)
	}
	payload := notify.BuildBatchPayload(trigger, results, startedAt, time.Now(), finMsg)
	notify.PublishBatchSummary(payload)
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

// noteCollectedMIDs 累计本源成功写入的 mid。
func noteCollectedMIDs(sourceID string, mids []int64) {
	if len(mids) == 0 {
		return
	}
	notify.Acc.Add(sourceID, mids...)
}
