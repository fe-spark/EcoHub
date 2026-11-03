package spider

import (
	"encoding/json"
	"fmt"
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
			status, errMsg = settleSourceStatus(status, errMsg, snap.Success)
			name := strings.TrimSpace(src.Name)
			if name == "" {
				name = strings.TrimSpace(snap.Name)
			}
			if name == "" {
				name = src.Id
			}
			detail = model.CollectRecordSourceDetail{
				SourceID:    src.Id,
				SourceName:  name,
				Status:      status,
				PageTotal:   snap.Total,
				PageCurrent: snap.Current,
				SuccessCnt:  snap.Success,
				FailedCnt:   snap.Failed,
				FilmsTotal:  countBySource[src.Id],
				Error:       errMsg,
			}
		} else {
			if count, ok := countBySource[src.Id]; ok && count > detail.FilmsTotal {
				detail.FilmsTotal = count
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

// 没有失败、但有站点被用户停止时，整批仍是停止。部分失败只留给确实失败的站点。
func batchCollectStatus(doneCount, failedCount, stoppedCount, total int) string {
	if total <= 0 || (failedCount == 0 && stoppedCount == 0 && doneCount == total) {
		return model.CollectRecordStatusSuccess
	}
	if failedCount == 0 && stoppedCount > 0 {
		return model.CollectRecordStatusStopped
	}
	if failedCount > 0 && doneCount > 0 {
		return model.CollectRecordStatusPartial
	}
	if failedCount > 0 {
		return model.CollectRecordStatusFailed
	}
	if doneCount == total {
		return model.CollectRecordStatusSuccess
	}
	return model.CollectRecordStatusFailed
}

func settleSourceStatus(status, errMsg string, successCnt ...int) (string, string) {
	if status == progress.StatusStopping {
		status = progress.StatusStopped
	}
	if status == progress.StatusStopped {
		return status, ""
	}
	if strings.TrimSpace(errMsg) != "" {
		if len(successCnt) > 0 && successCnt[0] > 0 {
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

	b.mu.Lock()
	const maxSampleFilmsPerSource = 20
	midsBySource := make(map[string][]int64, len(b.sourceNotifyMIDs))
	sampleMIDs := make(map[int64]struct{}, len(b.sourceNotifyMIDs)*maxSampleFilmsPerSource)

	for sid, set := range b.sourceNotifyMIDs {
		srcMids := make([]int64, 0, len(set))
		for m := range set {
			if m > 0 {
				srcMids = append(srcMids, m)
			}
		}
		sort.Slice(srcMids, func(i, j int) bool { return srcMids[i] < srcMids[j] })
		midsBySource[sid] = srcMids

		limit := len(srcMids)
		if limit > maxSampleFilmsPerSource {
			limit = maxSampleFilmsPerSource
		}
		for _, m := range srcMids[:limit] {
			sampleMIDs[m] = struct{}{}
		}
	}
	for _, m := range b.replayFilmIDs {
		if m > 0 {
			sampleMIDs[m] = struct{}{}
		}
	}
	b.mu.Unlock()

	// 性能安全防线：即便首次全量采集数十万部，也只查询抽样样本的片名（最多百量级），绝不全量查库防 OOM / SQL 超长
	sampleMIDList := make([]int64, 0, len(sampleMIDs))
	for m := range sampleMIDs {
		sampleMIDList = append(sampleMIDList, m)
	}
	var filmNames map[int64]string
	if len(sampleMIDList) > 0 {
		filmNames, _ = repository.FilmNamesByIDs(sampleMIDList)
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
		case progress.StatusDone, model.CollectRecordStatusSuccess:
			doneCount++
		case progress.StatusFailed, model.CollectRecordStatusPartial:
			failedCount++
		case progress.StatusStopped:
			stoppedCount++
		}

		var sourceFilms []model.CollectRecordFilm
		if srcMids, ok := midsBySource[r.SourceID]; ok && len(srcMids) > 0 {
			limit := len(srcMids)
			if limit > maxSampleFilmsPerSource {
				limit = maxSampleFilmsPerSource
			}
			sourceFilms = make([]model.CollectRecordFilm, 0, limit)
			for _, m := range srcMids[:limit] {
				name := filmNames[m]
				if name == "" {
					name = fmt.Sprintf("影片 #%d", m)
				}
				sourceFilms = append(sourceFilms, model.CollectRecordFilm{
					Mid:  m,
					Name: name,
				})
			}
		} else if len(b.replayFilmIDs) > 0 && (b.trigger == model.CollectTriggerSingleUpdate || b.tag == model.CollectTagSingleUpdate) {
			for _, m := range b.replayFilmIDs {
				name := filmNames[m]
				if name == "" {
					name = fmt.Sprintf("影片 #%d", m)
				}
				sourceFilms = append(sourceFilms, model.CollectRecordFilm{
					Mid:  m,
					Name: name,
				})
			}
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
			Films:       sourceFilms,
			Error:       r.Error,
		})
	}

	status := batchCollectStatus(doneCount, failedCount, stoppedCount, len(results))

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
		FailedSources:  failedCount,
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
		FailedSources:  failedCount,
		ErrorMsg:       errSummary.String(),
		DetailJson:     string(detailBytes),
	})
}
