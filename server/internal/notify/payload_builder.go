package notify

import (
	"strings"
	"time"

	"server/internal/model"
)

func BuildSourceResult(source model.FilmSource, progress model.CollectProgress, errMsg string) model.SourceNotifyResult {
	status := strings.TrimSpace(progress.Status)
	if status == "" {
		status = "done"
	}
	_, total, _ := Acc.DrainSource(source.Id)
	return model.SourceNotifyResult{
		SourceID:    source.Id,
		SourceName:  source.Name,
		Grade:       int(source.Grade),
		Status:      status,
		Error:       errMsg,
		PageTotal:   progress.Total,
		PageCurrent: progress.Current,
		SuccessCnt:  progress.Success,
		FailedCnt:   progress.Failed,
		FilmsTotal:  total,
	}
}

// BuildSourceResultDirect 不依赖进度快照时组装结果（如单片更新）。
func BuildSourceResultDirect(source model.FilmSource, status, errMsg string) model.SourceNotifyResult {
	if status == "" {
		status = "done"
	}
	_, total, _ := Acc.DrainSource(source.Id)
	successCnt, failedCnt := 0, 0
	if status == "done" {
		successCnt = 1
	} else if status == "failed" {
		failedCnt = 1
	}
	return model.SourceNotifyResult{
		SourceID:   source.Id,
		SourceName: source.Name,
		Grade:      int(source.Grade),
		Status:     status,
		Error:      errMsg,
		SuccessCnt: successCnt,
		FailedCnt:  failedCnt,
		FilmsTotal: total,
	}
}

// BuildBatchPayload 组装批次摘要。
// TotalFilms 优先用批次去重条数，否则分源 FilmsTotal 之和；ChangeBatchID 随批次显式写入。
func BuildBatchPayload(batch *ChangeBatch, trigger string, sources []model.SourceNotifyResult, startedAt, finishedAt time.Time, finalizeErr string) model.CollectBatchNotifyPayload {
	if finishedAt.IsZero() {
		finishedAt = time.Now()
	}
	var duration int64
	if !startedAt.IsZero() {
		duration = int64(finishedAt.Sub(startedAt).Seconds())
		if duration < 0 {
			duration = 0
		}
	}
	success, failed, sumSource := 0, 0, 0
	for _, s := range sources {
		switch s.Status {
		case "failed", "stopped":
			failed++
		case "done":
			success++
		default:
			if s.Status != "" {
				failed++
			}
		}
		sumSource += s.FilmsTotal
	}
	filmTotal := sumSource
	var films []model.FilmNotifyItem
	batchID := ""
	if batch != nil {
		if n := batch.Count(); n > 0 {
			filmTotal = n
		}
		items := batch.Items()
		if len(items) > 0 {
			films = make([]model.FilmNotifyItem, 0, len(items))
			for _, it := range items {
				films = append(films, model.FilmNotifyItem{
					Mid:        it.Mid,
					SourceName: it.SourceName,
				})
			}
		}
		batchID = batch.ID()
	}
	return model.CollectBatchNotifyPayload{
		Trigger:            trigger,
		StartedAt:          startedAt,
		FinishedAt:         finishedAt,
		DurationSec:        duration,
		Sources:            sources,
		TotalSources:       len(sources),
		SuccessSources:     success,
		FailedSources:      failed,
		TotalFilms:         filmTotal,
		IncludeFilmDetails: true,
		FinalizeError:      finalizeErr,
		Films:              films,
		ChangeBatchID:      batchID,
	}
}
