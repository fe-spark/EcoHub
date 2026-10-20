package spider

import "server/internal/model"

type recoverJob struct {
	source  model.FilmSource
	records []model.FailureRecord
}

// queueRecoverJobs 把已占用站点上的失败记录放进重试队列。已在队列中的记录跳过。
func queueRecoverJobs(recordsBySource map[string][]model.FailureRecord, sourceByID map[string]model.FilmSource, claimed map[string]struct{}) []recoverJob {
	jobs := make([]recoverJob, 0, len(claimed))
	for sourceID, records := range recordsBySource {
		if _, ok := claimed[sourceID]; !ok {
			continue
		}
		src, ok := sourceByID[sourceID]
		if !ok {
			continue
		}
		queued := make([]model.FailureRecord, 0, len(records))
		for i := range records {
			if TryBeginFailureRetry(records[i].ID) {
				queued = append(queued, records[i])
			}
		}
		if len(queued) > 0 {
			jobs = append(jobs, recoverJob{source: src, records: queued})
		}
	}
	return jobs
}
