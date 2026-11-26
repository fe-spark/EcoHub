package collect

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"server/internal/collect/ledger"
	"server/internal/collect/progress"
	"server/internal/infra/db"
	"server/internal/model"
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
	sourceHours := make(map[string]int, len(b.sourceHours))
	for sid, hour := range b.sourceHours {
		sourceHours[sid] = hour
	}
	batchHour := b.hour
	errBySource := make(map[string]string, len(b.sourceErrors))
	for k, v := range b.sourceErrors {
		errBySource[k] = v
	}
	sources := make([]model.FilmSource, 0, len(b.sources))
	for _, src := range b.sources {
		if b.sourceDroppedLocked(src.Id) {
			continue
		}
		sources = append(sources, src)
	}
	trigger := b.trigger
	startedAt := b.startedAt
	b.mu.Unlock()
	if len(sources) == 0 {
		return
	}

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
				FailedPages: copyFailedPages(snap.FailedPages),
				Error:       errMsg,
			}
		} else if detail.Hour == 0 {
			if hour := sourceHours[src.Id]; hour != 0 {
				detail.Hour = hour
			} else {
				detail.Hour = batchHour
			}
		}
		results = append(results, model.SourceNotifyResult{
			SourceID:     detail.SourceID,
			SourceName:   detail.SourceName,
			Sort:         src.Sort,
			Status:       detail.Status,
			Error:        detail.Error,
			PageTotal:    detail.PageTotal,
			PageCurrent:  detail.PageCurrent,
			SuccessCnt:   detail.SuccessCnt,
			FailedCnt:    detail.FailedCnt,
			FailedPages:  copyFailedPages(detail.FailedPages),
			FilmsTotal:   detail.FilmsTotal,
			CategorySkip: b.CategorySkip(src.Id),
		})
	}
	settledResults, ledgerSaved := persistCollectRecord(b, results)
	if len(settledResults) == 0 {
		return
	}

	d := getNotifyDispatcher()
	if d.OnBatchSummary == nil {
		return
	}

	notifyResults := make([]model.SourceNotifyResult, len(settledResults))
	for i, r := range settledResults {
		r.Error = stripCategorySkipError(r.Error, r.CategorySkip)
		notifyResults[i] = r
	}

	collectBatchID := ""
	if ledgerSaved {
		collectBatchID = b.batchID
	}

	d.OnBatchSummary(model.CollectBatchNotifyPayload{
		CollectBatchID: collectBatchID,
		Trigger:        trigger,
		StartedAt:      startedAt,
		FinishedAt:     time.Now(),
		Sources:        notifyResults,
	})
}

func stripCategorySkipError(errMsg string, unmapped int) string {
	if unmapped <= 0 || errMsg == "" {
		return errMsg
	}
	skipMsg := formatCategorySkip(unmapped)
	if errMsg == skipMsg {
		return ""
	}
	if strings.HasSuffix(errMsg, "；"+skipMsg) {
		return strings.TrimSuffix(errMsg, "；"+skipMsg)
	}
	return errMsg
}

func formatCategorySkip(unmapped int) string {
	if unmapped > 0 {
		return fmt.Sprintf("分类未映射，跳过 %d 部", unmapped)
	}
	return ""
}

func settleSourceStatus(status, errMsg string, successCnt int) (string, string) {
	if status == progress.StatusStopping {
		status = progress.StatusStopped
	}
	if status == progress.StatusSealing {
		// 待检索是成功采集的收尾阶段，记录按完成落库。
		status = progress.StatusDone
	}
	if status == progress.StatusStopped {
		return status, ""
	}
	if strings.TrimSpace(errMsg) != "" {
		if successCnt > 0 {
			return model.CollectRecordStatusPartial, errMsg
		}
		return progress.StatusFailed, errMsg
	}
	return status, errMsg
}

func persistCollectRecord(b *collectBatchContext, results []model.SourceNotifyResult) ([]model.SourceNotifyResult, bool) {
	if b == nil || strings.TrimSpace(b.batchID) == "" {
		return results, false
	}
	if err := b.recordStart(); err != nil {
		log.Printf("[Spider] 初始采集记录写入失败 batchID=%s: %v", b.batchID, err)
		return results, false
	}

	b.recordMu.Lock()
	defer b.recordMu.Unlock()

	finalFlushErr := b.flushLedgerLocked()

	b.mu.Lock()
	recordID := b.recordID
	flushErr := b.ledgerFlushErr
	ledgerWritten := b.ledgerWritten
	isSingleReplay := len(b.replayFilmIDs) > 0
	b.mu.Unlock()

	var bySource map[string]int
	ledgerReady := false
	if finalFlushErr != nil || flushErr != nil {
		err := flushErr
		if err == nil {
			err = finalFlushErr
		}
		log.Printf("[Spider] 采集账本写入异常，清理半截账本并继续结算终态 batchID=%s err=%v", b.batchID, err)
		b.cleanupIncompleteLedgerLocked()
	} else if ledgerWritten {
		storedTotal, counts, err := ledger.FilmRowCounts(db.Mdb, recordID)
		if err != nil {
			log.Printf("[Spider] 对账采集账本行数失败，清理半截账本并继续结算终态 batchID=%s err=%v", b.batchID, err)
			b.cleanupIncompleteLedgerLocked()
		} else {
			bySource = counts
			ledgerReady = storedTotal > 0
		}
	} else if isSingleReplay {
		// 单片更新所有站都未发生实质变更时，写入重放目标编号以支持列表展示片名与重试。
		if replayItems := b.singleReplayItemsLocked(); len(replayItems) > 0 {
			if err := ledger.AppendCollectRecordFilms(replayItems); err != nil {
				log.Printf("[Spider] 单片重放账本写入失败，继续结算终态 batchID=%s err=%v", b.batchID, err)
				b.cleanupIncompleteLedgerLocked()
			} else {
				ledgerReady = true
			}
		}
	}

	if next, removed := b.withoutDroppedResults(results); removed {
		results = next
	}

	totalPage := 0
	succPage := 0
	failPage := 0
	filmCount := 0
	doneCount := 0
	partialCount := 0
	failedCount := 0
	stoppedCount := 0
	interruptedCount := 0

	details := make([]model.CollectRecordSourceDetail, 0, len(results))
	for i := range results {
		status, errMsg := settleSourceStatus(results[i].Status, results[i].Error, results[i].SuccessCnt)
		results[i].Status = status
		results[i].Error = errMsg
		if bySource != nil {
			results[i].FilmsTotal = bySource[results[i].SourceID]
		}
		r := results[i]
		totalPage += r.PageTotal
		succPage += r.SuccessCnt
		failPage += r.FailedCnt
		filmCount += r.FilmsTotal

		switch r.Status {
		case progress.StatusDone, model.CollectRecordStatusSuccess:
			doneCount++
		case model.CollectRecordStatusPartial:
			partialCount++
		case progress.StatusFailed:
			failedCount++
		case progress.StatusStopped:
			stoppedCount++
		case model.CollectRecordStatusInterrupted:
			interruptedCount++
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
			FailedPages: copyFailedPages(r.FailedPages),
			FilmsTotal:  r.FilmsTotal,
			Error:       r.Error,
		})
	}

	status := model.CollectBatchStatus(doneCount, partialCount, failedCount, stoppedCount, interruptedCount, len(results), succPage, failPage)

	detailBytes, err := json.Marshal(details)
	if err != nil {
		log.Printf("[Spider] 采集明细序列化失败 batchID=%s: %v", b.batchID, err)
		return results, false
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

	comp := model.CollectRecordCompletion{
		Status:         status,
		Hour:           b.hour,
		SourceCount:    len(results),
		SuccessSources: doneCount,
		FailedSources:  failedCount + partialCount,
		FinishedAt:     now,
		DurationMs:     duration,
		FilmCount:      filmCount,
		PageTotal:      totalPage,
		PageSuccess:    succPage,
		PageFailed:     failPage,
		ErrorMsg:       errSummary.String(),
		DetailJson:     string(detailBytes),
		Ledger:         ledgerReady,
	}

	log.Printf("[Spider] 结算采集记录 batchID=%s status=%s sources=%d done=%d partial=%d failed=%d stopped=%d interrupted=%d pageTotal=%d pageSuccess=%d pageFailed=%d filmRows=%d durationMs=%d err=%s",
		b.batchID, status, len(results), doneCount, partialCount, failedCount, stoppedCount, interruptedCount, totalPage, succPage, failPage, filmCount, duration, errSummary.String())

	var completeErr error
	for attempt := 0; attempt < 3; attempt++ {
		if completeErr = ledger.CompleteCollectRecord(b.batchID, comp); completeErr == nil {
			return results, ledgerReady
		}
		if attempt < 2 {
			time.Sleep(time.Duration(200<<attempt) * time.Millisecond)
		}
	}
	log.Printf("[Spider] 结算采集记录写入失败 batchID=%s status=%s err=%v", b.batchID, status, completeErr)
	b.cleanupIncompleteLedgerLocked()
	return results, false
}

func (b *collectBatchContext) cleanupIncompleteLedgerLocked() {
	b.mu.Lock()
	recordID := b.recordID
	b.mu.Unlock()
	if recordID == 0 {
		return
	}
	if err := ledger.DeleteCollectRecordFilms(recordID); err != nil {
		log.Printf("[Spider] 清理未结算账本失败 batchID=%s recordID=%d err=%v", b.batchID, recordID, err)
	}
}
