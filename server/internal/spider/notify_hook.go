package spider

import (
	"encoding/json"
	"log"
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
	countBySource := make(map[string]int, len(b.sourceNotifyMIDs))
	for sid, set := range b.sourceNotifyMIDs {
		countBySource[sid] = len(set)
	}
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
		status, errMsg = settleSourceStatus(status, errMsg)
		name := strings.TrimSpace(src.Name)
		if name == "" {
			name = strings.TrimSpace(snap.Name)
		}
		if name == "" {
			name = src.Id
		}
		results = append(results, model.SourceNotifyResult{
			SourceID:    src.Id,
			SourceName:  name,
			Sort:        src.Sort,
			Status:      status,
			Error:       errMsg,
			PageTotal:   snap.Total,
			PageCurrent: snap.Current,
			SuccessCnt:  snap.Success,
			FailedCnt:   snap.Failed,
			FilmsTotal:  countBySource[src.Id],
		})
	}
	persistCollectRecord(b, results, mids, errBySource)

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

func settleSourceStatus(status, errMsg string) (string, string) {
	if status == progress.StatusStopping {
		status = progress.StatusStopped
	}
	if status == progress.StatusStopped {
		return status, ""
	}
	if strings.TrimSpace(errMsg) != "" {
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

	totalPage := 0
	succPage := 0
	failPage := 0
	doneCount := 0
	failedCount := 0
	stoppedCount := 0

	details := make([]model.CollectRecordSourceDetail, 0, len(results))
	for _, r := range results {
		totalPage += r.PageTotal
		succPage += r.SuccessCnt
		failPage += r.FailedCnt

		switch r.Status {
		case progress.StatusDone:
			doneCount++
		case progress.StatusFailed:
			failedCount++
		case progress.StatusStopped:
			stoppedCount++
		}

		details = append(details, model.CollectRecordSourceDetail{
			SourceID:    r.SourceID,
			SourceName:  r.SourceName,
			Status:      r.Status,
			PageTotal:   r.PageTotal,
			PageCurrent: r.PageCurrent,
			SuccessCnt:  r.SuccessCnt,
			FailedCnt:   r.FailedCnt,
			FilmsTotal:  r.FilmsTotal,
			Error:       r.Error,
		})
	}

	status := model.CollectRecordStatusSuccess
	if len(results) > 0 {
		if doneCount == len(results) {
			status = model.CollectRecordStatusSuccess
		} else if stoppedCount == len(results) {
			status = model.CollectRecordStatusStopped
		} else if failedCount == len(results) {
			status = model.CollectRecordStatusFailed
		} else if doneCount > 0 {
			status = model.CollectRecordStatusPartial
		} else {
			status = model.CollectRecordStatusFailed
		}
	}

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

	comp := model.CollectRecordCompletion{
		Status:         status,
		Hour:           b.hour,
		SuccessSources: doneCount,
		FailedSources:  failedCount,
		FinishedAt:     now,
		DurationMs:     duration,
		FilmCount:      len(mids),
		PageTotal:      totalPage,
		PageSuccess:    succPage,
		PageFailed:     failPage,
		ErrorMsg:       errSummary.String(),
		DetailJson:     string(detailBytes),
	}

	if err := repository.CompleteCollectRecord(b.batchID, comp); err != nil {
		log.Printf("[Spider] 结算采集记录失败 batchID=%s: %v", b.batchID, err)
	}
}
