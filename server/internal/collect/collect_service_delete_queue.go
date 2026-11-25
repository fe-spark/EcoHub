package collect

import (
	"server/internal/repobase"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/infra/syslog"
	"server/internal/model"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/collect/progress"
)

type deleteSourceJob struct {
	id                    string
	token                 uint64
	clearPrimaryOnDequeue bool
}

var (
	deleteSourceQueue      = make(chan deleteSourceJob, 1000)
	deleteSourceStateMu    sync.Mutex
	deleteJobSeq           atomic.Uint64
	activeDeletingSourceID string
	reservedDeleteSources  = make(map[string]*delSourceReservation)
	queuedDeleteJobs       = make(map[string]uint64)
	canceledDeleteTokens   = make(map[uint64]struct{})
)

func init() {
	go runDeleteSourceWorker()
}

func runDeleteSourceWorker() {
	for job := range deleteSourceQueue {
		func(j deleteSourceJob) {
			defer deleteSourceJobs.Done()
			processAsyncDeleteSource(j)
		}(job)
	}
}

func executeDeferredPrimaryClear(id string) error {
	deadline := time.Now().Add(deleteIndexWait)
	for {
		var needRetry bool
		err := filmsnapshot.WithPrimaryMutation(func(searchBusy bool) error {
			storedID := repobase.StoredPrimarySourceID()
			if storedID != "" && storedID != id {
				return nil
			}
			others := repobase.FindOtherEnabledCollectSourcesExcluding(map[string]struct{}{id: {}})
			if len(others) > 0 {
				return errors.New("已有其他启用站，请重新指定新主站后再删除当前主站")
			}
			if searchBusy {
				needRetry = true
				return filmsnapshot.ErrSearchIndexBusy
			}
			oldPrimary := repobase.FindCollectSourceById(id)
			return applyPrimaryHandover(oldPrimary, id, "", true)
		})
		if err == nil {
			return nil
		}
		if !needRetry || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func processAsyncDeleteSource(job deleteSourceJob) {
	id := job.id
	deleteSourceStateMu.Lock()
	if _, canceled := canceledDeleteTokens[job.token]; canceled {
		delete(canceledDeleteTokens, job.token)
		deleteSourceStateMu.Unlock()
		syslog.Infof("[Collect] 异步删除站点已在排队中被取消，跳过清理 id=%s", id)
		return
	}
	if curTok, ok := queuedDeleteJobs[id]; ok && curTok == job.token {
		delete(queuedDeleteJobs, id)
	}
	activeDeletingSourceID = id
	deleteSourceStateMu.Unlock()

	defer func() {
		deleteSourceStateMu.Lock()
		if activeDeletingSourceID == id {
			activeDeletingSourceID = ""
		}
		deleteSourceStateMu.Unlock()
		progress.SetSourceDeleting(id, false)
	}()

	src := repobase.FindCollectSourceForDelete(id)
	name := id
	if src != nil && src.Name != "" {
		name = src.Name
	}

	if job.clearPrimaryOnDequeue {
		if err := waitDeleteIdle(id, deleteIndexWait); err != nil {
			syslog.Errorf("[Collect] 异步删除主站等待检索空闲超时 id=%s name=%s err=%v", id, name, err)
			progress.BroadcastSourceDeleteStatus(id, name, "failed", err.Error(), 0)
			return
		}
		if err := executeDeferredPrimaryClear(id); err != nil {
			syslog.Errorf("[Collect] 异步删除主站置空交接失败 id=%s name=%s err=%v", id, name, err)
			progress.BroadcastSourceDeleteStatus(id, name, "failed", err.Error(), 0)
			return
		}
	}

	syslog.Infof("[Collect] 异步删除站点开始 id=%s name=%s", id, name)
	started := time.Now()
	progress.BroadcastSourceDeleteStatus(id, name, "deleting", "", 0)
	deletedFilms, err := CollectSvc.delFilmSource(id)
	if err != nil {
		syslog.Errorf("[Collect] 异步删除站点失败 id=%s name=%s 耗时=%s err=%v", id, name, time.Since(started).Round(time.Millisecond), err)
		progress.BroadcastSourceDeleteStatus(id, name, "failed", err.Error(), 0)
		return
	}
	syslog.Infof("[Collect] 异步删除站点完成 id=%s name=%s 删除影片=%d 耗时=%s", id, name, deletedFilms, time.Since(started).Round(time.Millisecond))
	progress.BroadcastSourceDeleteStatus(id, name, "deleted", "", deletedFilms)
}

// CancelDelFilmSources 撤回排队中尚未开始删除的站点。正在物理清理中的站点不受影响继续安全完成。
func (s *CollectService) CancelDelFilmSources(ids []string) []string {
	deleteSourceStateMu.Lock()
	defer deleteSourceStateMu.Unlock()

	activeID := activeDeletingSourceID

	targetIDs := ids
	if len(targetIDs) == 0 {
		targetIDs = progress.GetDeletingSourceIDs()
	}

	canceled := make([]string, 0, len(targetIDs))
	for _, rawID := range targetIDs {
		id := strings.TrimSpace(rawID)
		if id == "" || id == activeID {
			continue
		}
		if !progress.IsSourceDeleting(id) {
			continue
		}
		if res, ok := reservedDeleteSources[id]; ok {
			res.canceledIDs[id] = struct{}{}
		}
		if tok, ok := queuedDeleteJobs[id]; ok {
			delete(queuedDeleteJobs, id)
			canceledDeleteTokens[tok] = struct{}{}
		}
		name := id
		if src := repobase.FindCollectSourceById(id); src != nil && src.Name != "" {
			name = src.Name
		}
		progress.BroadcastSourceDeleteStatus(id, name, "canceled", "已取消删除", 0)
		canceled = append(canceled, id)
	}
	if len(canceled) > 0 {
		syslog.Infof("[Collect] 已取消 %d 个站点的待删除排队: %v (当前活跃执行站: %s)", len(canceled), canceled, activeID)
	}
	return canceled
}

type delSourceReservation struct {
	queue       chan deleteSourceJob
	sources     []*model.FilmSource
	canceledIDs map[string]struct{}
}

func reserveDelFilmSources(queue chan deleteSourceJob, sources []*model.FilmSource) (*delSourceReservation, error) {
	deleteSourceStateMu.Lock()
	defer deleteSourceStateMu.Unlock()

	if cap(queue)-len(queue)-len(reservedDeleteSources) < len(sources) {
		return nil, errors.New("删除队列已满")
	}
	for _, src := range sources {
		if progress.IsSourceDeleting(src.Id) {
			return nil, errors.New("该站正在删除")
		}
	}
	res := &delSourceReservation{
		queue:       queue,
		sources:     sources,
		canceledIDs: make(map[string]struct{}),
	}
	for _, src := range sources {
		progress.SetSourceDeleting(src.Id, true)
		reservedDeleteSources[src.Id] = res
	}
	return res, nil
}

func (r *delSourceReservation) rollback() {
	if r == nil {
		return
	}
	deleteSourceStateMu.Lock()
	defer deleteSourceStateMu.Unlock()
	for _, src := range r.sources {
		delete(reservedDeleteSources, src.Id)
		progress.SetSourceDeleting(src.Id, false)
	}
}

func (r *delSourceReservation) commit(orderedSources []*model.FilmSource, clearPrimarySourceID string) {
	if r == nil {
		return
	}
	deleteSourceStateMu.Lock()
	defer deleteSourceStateMu.Unlock()

	sourcesToEnqueue := orderedSources
	if len(sourcesToEnqueue) == 0 {
		sourcesToEnqueue = r.sources
	}
	for _, src := range sourcesToEnqueue {
		delete(reservedDeleteSources, src.Id)
		if _, canceled := r.canceledIDs[src.Id]; canceled {
			continue
		}
		tok := deleteJobSeq.Add(1)
		queuedDeleteJobs[src.Id] = tok
		deleteSourceJobs.Add(1)
		progress.BroadcastSourceDeleteStatus(src.Id, src.Name, "deleting", "", 0)
		r.queue <- deleteSourceJob{
			id:                    src.Id,
			token:                 tok,
			clearPrimaryOnDequeue: clearPrimarySourceID != "" && src.Id == clearPrimarySourceID,
		}
	}
}

func enqueueDelFilmSources(queue chan deleteSourceJob, sources []*model.FilmSource) error {
	res, err := reserveDelFilmSources(queue, sources)
	if err != nil {
		return err
	}
	res.commit(nil, "")
	return nil
}

func enqueueDelFilmSource(src *model.FilmSource) error {
	return enqueueDelFilmSources(deleteSourceQueue, []*model.FilmSource{src})
}
