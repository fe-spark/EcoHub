package spider

import (
	"encoding/json"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider/progress"
)

// NotifyDispatcher 采集与定时任务通知回调集合（由 service 层注入，避免循环依赖）
type NotifyDispatcher struct {
	OnBatchSummary func(payload model.CollectBatchNotifyPayload)
	OnSourceFailed func(sourceID, sourceName, reason string)
	OnCronFailed   func(taskID, remark, reason string)
}

var (
	notifyDispatcherMu sync.RWMutex
	notifyDispatcher   NotifyDispatcher
)

// RegisterNotifyDispatcher 注册通知分发器回调
func RegisterNotifyDispatcher(d NotifyDispatcher) {
	notifyDispatcherMu.Lock()
	notifyDispatcher = d
	notifyDispatcherMu.Unlock()
}

func getNotifyDispatcher() NotifyDispatcher {
	notifyDispatcherMu.RLock()
	d := notifyDispatcher
	notifyDispatcherMu.RUnlock()
	return d
}

func emitSourceFailedNotify(sourceID, sourceName, reason string) {
	d := getNotifyDispatcher()
	if d.OnSourceFailed == nil {
		return
	}
	sourceID = strings.TrimSpace(sourceID)
	sourceName = strings.TrimSpace(sourceName)
	if sourceName == "" {
		sourceName = sourceID
	}
	d.OnSourceFailed(sourceID, sourceName, strings.TrimSpace(reason))
}

func emitCronFailedNotify(taskID, remark, reason string) {
	d := getNotifyDispatcher()
	if d.OnCronFailed == nil {
		return
	}
	d.OnCronFailed(strings.TrimSpace(taskID), strings.TrimSpace(remark), strings.TrimSpace(reason))
}

func emitBatchNotify(b *collectBatchContext) {
	if b == nil || len(b.sources) == 0 {
		return
	}
	b.mu.Lock()
	mids := make([]int64, 0, len(b.notifyMIDs))
	for mid := range b.notifyMIDs {
		if mid > 0 {
			mids = append(mids, mid)
		}
	}
	filmMids := make([]int64, 0, len(b.filmMIDs))
	for mid := range b.filmMIDs {
		if mid > 0 {
			filmMids = append(filmMids, mid)
		}
	}
	countBySource := make(map[string]int, len(b.sourceFilmMIDs))
	for sid, set := range b.sourceFilmMIDs {
		countBySource[sid] = len(set)
	}
	sourceHours := make(map[string]int, len(b.sourceHours))
	for sid, hour := range b.sourceHours {
		sourceHours[sid] = hour
	}
	batchHour := b.hour
	errBySource := make(map[string]string, len(b.sourceErrors))
	for k, v := range b.sourceErrors {
		errBySource[k] = v
	}
	sources := append([]model.FilmSource(nil), b.sources...)
	trigger := b.trigger
	startedAt := b.startedAt
	b.mu.Unlock()

	results := make([]model.SourceNotifyResult, 0, len(sources))
	for _, src := range sources {
		detail, hasDetail := b.getSourceSnapshot(src.Id)
		if !hasDetail {
			snap, ok := progress.Snapshot(src.Id)
			status := snap.Status
			errMsg := strings.TrimSpace(errBySource[src.Id])
			if !ok {
				if errMsg != "" {
					status = progress.StatusFailed
				} else {
					status = progress.StatusDone
				}
			}
			status, errMsg = settleSourceStatus(status, errMsg, snap.Success, snap.Reason)
			name := strings.TrimSpace(src.Name)
			if name == "" {
				name = strings.TrimSpace(snap.Name)
			}
			if name == "" {
				name = src.Id
			}
			hour := sourceHours[src.Id]
			if hour == 0 {
				hour = batchHour
			}
			detail = model.CollectRecordSourceDetail{
				SourceID:    src.Id,
				SourceName:  name,
				Status:      status,
				Hour:        hour,
				PageTotal:   snap.Total,
				PageCurrent: snap.Success,
				SuccessCnt:  snap.Success,
				FailedCnt:   snap.Failed,
				FilmsTotal:  countBySource[src.Id],
				Error:       errMsg,
			}
		} else {
			if count, ok := countBySource[src.Id]; ok && count > detail.FilmsTotal {
				detail.FilmsTotal = count
			}
			if detail.Hour == 0 {
				if hour := sourceHours[src.Id]; hour != 0 {
					detail.Hour = hour
				} else {
					detail.Hour = batchHour
				}
			}
		}
		results = append(results, model.SourceNotifyResult{
			SourceID:    detail.SourceID,
			SourceName:  detail.SourceName,
			Sort:        src.Sort,
			Status:      detail.Status,
			Error:       detail.Error,
			PageTotal:   detail.PageTotal,
			PageCurrent: detail.PageCurrent,
			SuccessCnt:  detail.SuccessCnt,
			FailedCnt:   detail.FailedCnt,
			FilmsTotal:  detail.FilmsTotal,
		})
	}
	persistCollectRecord(b, results, filmMids, errBySource)

	d := getNotifyDispatcher()
	if d.OnBatchSummary == nil {
		return
	}

	d.OnBatchSummary(model.CollectBatchNotifyPayload{
		Trigger:    trigger,
		StartedAt:  startedAt,
		FinishedAt: time.Now(),
		Sources:    results,
		NotifyMIDs: mids,
	})
}

// batchCollectStatus 判定批次最终状态。
// 规则：
//  1. 全部成功 (success)：所有站点无失败、无停止、无异常，且完成站数等于总站数，无失败页。
//  2. 已终止 (stopped)：没有失败或部分失败的站点，仅存在用户手动停止的站点。
//     停止前已抓取的失败页不计为失败，避免用户主动停止被误判为部分失败。
//  3. 部分失败 (partial)：存在失败或异常（页失败或站点失败），且同时存在有效成功产出（成功页或成功站）。
//  4. 采集失败 (failed)：存在失败且完全无任何成功产出（零页成功、零站成功）。
func batchCollectStatus(doneCount, partialCount, failedCount, stoppedCount, total int, succPages, failPages int) string {
	if total <= 0 || (failedCount == 0 && partialCount == 0 && stoppedCount == 0 && doneCount == total && failPages == 0) {
		return model.CollectRecordStatusSuccess
	}
	if failedCount == 0 && partialCount == 0 && stoppedCount > 0 {
		return model.CollectRecordStatusStopped
	}
	if failPages > 0 || failedCount > 0 || partialCount > 0 {
		if succPages > 0 || doneCount > 0 || partialCount > 0 {
			return model.CollectRecordStatusPartial
		}
		return model.CollectRecordStatusFailed
	}
	if doneCount == total {
		return model.CollectRecordStatusSuccess
	}
	return model.CollectRecordStatusFailed
}

func settleSourceStatus(status, errMsg string, successCnt int, reason string) (string, string) {
	if status == progress.StatusStopping {
		status = progress.StatusStopped
	}
	if status == progress.StatusStopped {
		return status, ""
	}
	// 连续失败就是失败。已经写出的页留在成功页里，不把站点改成部分失败。
	if reason == model.CollectFailConsecutive {
		return progress.StatusFailed, strings.TrimSpace(errMsg)
	}
	if strings.TrimSpace(errMsg) != "" {
		if successCnt > 0 {
			return model.CollectRecordStatusPartial, errMsg
		}
		return progress.StatusFailed, errMsg
	}
	return status, errMsg
}

func persistCollectRecord(b *collectBatchContext, results []model.SourceNotifyResult, mids []int64, _ map[string]string) {
	if b == nil || strings.TrimSpace(b.batchID) == "" {
		return
	}
	if err := b.recordStart(); err != nil {
		log.Printf("[Spider] 初始采集记录写入失败 batchID=%s: %v", b.batchID, err)
		return
	}
	b.flushRecordSync()

	b.mu.Lock()
	midsBySource := make(map[string][]int64, len(b.sourceFilmMIDs))
	for sid, set := range b.sourceFilmMIDs {
		srcMids := make([]int64, 0, len(set))
		for m := range set {
			if m > 0 {
				srcMids = append(srcMids, m)
			}
		}
		sort.Slice(srcMids, func(i, j int) bool { return srcMids[i] < srcMids[j] })
		midsBySource[sid] = srcMids
	}
	b.mu.Unlock()

	totalPage := 0
	succPage := 0
	failPage := 0
	doneCount := 0
	partialCount := 0
	failedCount := 0
	stoppedCount := 0

	details := make([]model.CollectRecordSourceDetail, 0, len(results))
	for _, r := range results {
		totalPage += r.PageTotal
		succPage += r.SuccessCnt
		failPage += r.FailedCnt

		switch r.Status {
		case progress.StatusDone, model.CollectRecordStatusSuccess:
			doneCount++
		case model.CollectRecordStatusPartial:
			partialCount++
		case progress.StatusFailed:
			failedCount++
		case progress.StatusStopped:
			stoppedCount++
		}

		details = append(details, model.CollectRecordSourceDetail{
			SourceID:    r.SourceID,
			SourceName:  r.SourceName,
			Status:      r.Status,
			Hour:        b.sourceHour(r.SourceID),
			PageTotal:   r.PageTotal,
			PageCurrent: r.PageCurrent,
			SuccessCnt:  r.SuccessCnt,
			FailedCnt:   r.FailedCnt,
			FilmsTotal:  r.FilmsTotal,
			Error:       r.Error,
		})
	}

	status := batchCollectStatus(doneCount, partialCount, failedCount, stoppedCount, len(results), succPage, failPage)

	detailBytes, err := json.Marshal(details)
	if err != nil {
		log.Printf("[Spider] 采集明细序列化失败 batchID=%s: %v", b.batchID, err)
		return
	}

	var errSummary strings.Builder
	for _, r := range results {
		if errMsg := strings.TrimSpace(r.Error); errMsg != "" {
			if errSummary.Len() > 0 {
				errSummary.WriteString("; ")
			}
			errSummary.WriteString(r.SourceName + ": " + errMsg)
			if errSummary.Len() > 500 {
				break
			}
		}
	}

	now := time.Now()
	duration := now.Sub(b.startedAt).Milliseconds()
	if duration < 0 {
		duration = 0
	}

	// 单片更新记录影片ID供重放；常规大批量采集不往 film_ids 塞海量字符串防超过字段长度限制
	batchFilmIDs := formatReplayFilmIDs(b.replayFilmIDs)

	comp := model.CollectRecordCompletion{
		Status:         status,
		Hour:           b.hour,
		SuccessSources: doneCount,
		FailedSources:  failedCount + partialCount,
		FinishedAt:     now,
		DurationMs:     duration,
		FilmCount:      len(mids),
		FilmIDs:        batchFilmIDs,
		PageTotal:      totalPage,
		PageSuccess:    succPage,
		PageFailed:     failPage,
		ErrorMsg:       errSummary.String(),
		DetailJson:     string(detailBytes),
	}

	if err := repository.CompleteCollectRecord(b.batchID, comp); err != nil {
		log.Printf("[Spider] 结算采集记录失败 batchID=%s: %v", b.batchID, err)
	}

	// 清单只记各站实际入库的 mid。重放影片留在记录的 film_ids 上，供重试和历史跳转。
	if len(midsBySource) > 0 {
		if err := repository.BatchSaveCollectRecordFilms(b.batchID, midsBySource); err != nil {
			log.Printf("[Spider] 保存采集批次影片清单失败 batchID=%s: %v", b.batchID, err)
		}
	}

	BroadcastRecordCompletion(&model.CollectRecordProgressPayload{
		BatchID:        b.batchID,
		Status:         status,
		FinishedAt:     &now,
		DurationMs:     duration,
		FilmCount:      len(mids),
		PageTotal:      totalPage,
		PageSuccess:    succPage,
		PageFailed:     failPage,
		SuccessSources: doneCount,
		FailedSources:  failedCount + partialCount,
		ErrorMsg:       errSummary.String(),
		DetailJson:     string(detailBytes),
	})
}
