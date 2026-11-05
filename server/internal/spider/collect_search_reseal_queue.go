package spider

import (
	"log"
	"strings"
	"sync"

	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider/progress"
)

const sealCoalesceMax = 20000

type sealSiteJob struct {
	sourceID string
	name     string
	mids     []int64
}

// searchResealQueue 全局一条封口队列。采集队列有几条都排进这里，单协程依次封。
type searchResealQueue struct {
	mu      sync.Mutex
	pending []sealSiteJob
	sealing []sealSiteJob
	running bool
}

var searchReseal = &searchResealQueue{}

func scheduleSearchResealSite(sourceID, name string, mids []int64) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || len(mids) == 0 {
		return
	}
	scheduleSearchResealJob(sealSiteJob{sourceID: sourceID, name: name, mids: mids})
}

func scheduleSearchResealRemain(mids []int64) {
	if len(mids) == 0 {
		return
	}
	scheduleSearchResealJob(sealSiteJob{mids: append([]int64(nil), mids...)})
}

func scheduleSearchResealJob(job sealSiteJob) {
	if len(job.mids) == 0 {
		return
	}
	searchReseal.mu.Lock()
	searchReseal.pending, job = absorbPendingSameSource(searchReseal.pending, job)
	searchReseal.pending = append(searchReseal.pending, job)
	start := !searchReseal.running
	if start {
		searchReseal.running = true
	}
	searchReseal.mu.Unlock()
	if start {
		go searchReseal.run()
		return
	}
	searchReseal.publishSites()
}

func (q *searchResealQueue) run() {
	for {
		q.mu.Lock()
		taken, rest := coalesceSealJobs(q.pending, sealCoalesceMax)
		q.pending = rest
		q.sealing = taken
		if len(taken) == 0 {
			q.running = false
			progress.SetSearchSealSites(nil)
			q.mu.Unlock()
			return
		}
		q.mu.Unlock()
		q.publishSites()

		mids := flattenSealMIDs(taken)
		applied := false
		panicked := false
		func() {
			defer func() {
				if r := recover(); r != nil {
					panicked = true
					log.Printf("[Spider][SearchReseal] 封口失败 panic=%v", r)
				}
			}()
			applied = filmsnapshot.ResealSearchIndexMids(mids)
		}()

		q.mu.Lock()
		if len(q.pending) > 0 {
			q.mu.Unlock()
			continue
		}
		q.sealing = nil
		q.running = false
		progress.SetSearchSealSites(nil)
		p := filmsnapshot.GetLatestSearchIndexProgress()
		switch {
		case applied:
			filmsnapshot.NotifySearchIndexProgress(p.Total, p.Total, "done")
		case !panicked && filmsnapshot.SearchRebuildScheduled():
		default:
			filmsnapshot.NotifySearchIndexProgress(0, 0, "idle")
		}
		q.mu.Unlock()
		return
	}
}

func (q *searchResealQueue) publishSites() {
	q.mu.Lock()
	progress.SetSearchSealSites(sealQueueSites(q.sealing, q.pending))
	q.mu.Unlock()
	p := filmsnapshot.GetLatestSearchIndexProgress()
	status := p.Status
	if status == "" || status == "idle" || status == "done" {
		status = "indexing"
	}
	filmsnapshot.NotifySearchIndexProgress(p.Total, p.Current, status)
}

func flattenSealMIDs(jobs []sealSiteJob) []int64 {
	n := 0
	for _, job := range jobs {
		n += len(job.mids)
	}
	out := make([]int64, 0, n)
	for _, job := range jobs {
		out = append(out, job.mids...)
	}
	return out
}

// absorbPendingSameSource 把同一站还在排队的任务撤掉，编号并进这次入队的任务。
// 正在封的任务不在 pending 里，不会被并进来。
func absorbPendingSameSource(pending []sealSiteJob, job sealSiteJob) ([]sealSiteJob, sealSiteJob) {
	if job.sourceID == "" {
		return pending, job
	}
	kept := make([]sealSiteJob, 0, len(pending))
	var prior []int64
	for _, old := range pending {
		if old.sourceID != job.sourceID {
			kept = append(kept, old)
			continue
		}
		prior = append(prior, old.mids...)
	}
	if len(prior) == 0 {
		return pending, job
	}
	job.mids = unionSealMIDs(prior, job.mids)
	return kept, job
}

func unionSealMIDs(groups ...[]int64) []int64 {
	seen := make(map[int64]struct{})
	out := make([]int64, 0)
	for _, group := range groups {
		for _, mid := range group {
			if _, ok := seen[mid]; ok {
				continue
			}
			seen[mid] = struct{}{}
			out = append(out, mid)
		}
	}
	return out
}

func coalesceSealJobs(jobs []sealSiteJob, max int) (taken, rest []sealSiteJob) {
	if len(jobs) == 0 || max <= 0 {
		return nil, append([]sealSiteJob(nil), jobs...)
	}
	total := 0
	cut := 0
	for i, job := range jobs {
		n := len(job.mids)
		if i > 0 && total+n > max {
			break
		}
		total += n
		cut = i + 1
		if total >= max {
			break
		}
	}
	if cut == 0 {
		return nil, append([]sealSiteJob(nil), jobs...)
	}
	return append([]sealSiteJob(nil), jobs[:cut]...), append([]sealSiteJob(nil), jobs[cut:]...)
}

func sealQueueSites(sealing, waiting []sealSiteJob) []progress.SearchSealSite {
	out := make([]progress.SearchSealSite, 0, len(sealing)+len(waiting))
	for _, job := range sealing {
		if job.sourceID == "" {
			continue
		}
		out = append(out, progress.SearchSealSite{SourceID: job.sourceID, State: "sealing", Ahead: 0})
	}
	ahead := len(sealing)
	for _, job := range waiting {
		if job.sourceID == "" {
			ahead++
			continue
		}
		out = append(out, progress.SearchSealSite{
			SourceID: job.sourceID,
			State:    "waiting",
			Ahead:    ahead,
		})
		ahead++
	}
	return out
}
