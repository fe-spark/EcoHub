package spider

import (
	"strings"
	"sync"
	"time"

	"server/internal/model"
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
	d := getNotifyDispatcher()
	if d.OnBatchSummary == nil {
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
		if errMsg != "" && status != progress.StatusStopped {
			status = progress.StatusFailed
		}
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

	d.OnBatchSummary(model.CollectBatchNotifyPayload{
		Trigger:    trigger,
		StartedAt:  startedAt,
		FinishedAt: time.Now(),
		Sources:    results,
		NotifyMIDs: mids,
	})
}
