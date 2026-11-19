package service

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider"
	"server/internal/spider/progress"
)

// deleteIndexWait 删除前等待已经在进行的检索索引、封口和主站预热。
const deleteIndexWait = 30 * time.Minute

func deleteIdleReason() string {
	reason := spider.SearchWorkBusyReason()
	if primaryWarmCount.Load() <= 0 {
		return reason
	}
	if reason == "" {
		return "主站预热"
	}
	return reason + "，主站预热"
}

func waitDeleteIdle(id string, timeout time.Duration) error {
	reason := deleteIdleReason()
	if reason == "" {
		syslog.Infof("[Collect] 删除站点检索空闲 id=%s", id)
		return nil
	}
	if timeout <= 0 {
		syslog.Errorf("[Collect] 删除站点检索仍在忙且无等待上限 id=%s 原因=%s", id, reason)
		return errors.New("检索索引更新超时，未删除")
	}
	syslog.Infof("[Collect] 删除站点等待检索结束 id=%s 原因=%s 上限=%s", id, reason, timeout)
	started := time.Now()
	deadline := time.Now().Add(timeout)
	for {
		reason = deleteIdleReason()
		if reason == "" {
			syslog.Infof("[Collect] 删除站点检索已结束 id=%s 耗时=%s", id, time.Since(started).Round(time.Millisecond))
			return nil
		}
		if !time.Now().Before(deadline) {
			syslog.Errorf("[Collect] 删除站点等待检索超时 id=%s 仍在=%s 耗时=%s", id, reason, time.Since(started).Round(time.Millisecond))
			return errors.New("检索索引更新超时，未删除")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

var (
	deleteSourceQueue      = make(chan string, 1000)
	deleteSourceEnqueueMu  sync.Mutex
	deleteSourceDispatchMu sync.Mutex
	activeDeletingSourceID atomic.Pointer[string]
	canceledSourceDeletes  sync.Map
)

func init() {
	go runDeleteSourceWorker()
}

func runDeleteSourceWorker() {
	for id := range deleteSourceQueue {
		func(sourceID string) {
			defer deleteSourceJobs.Done()
			processAsyncDeleteSource(sourceID)
		}(id)
	}
}

func processAsyncDeleteSource(id string) {
	deleteSourceDispatchMu.Lock()
	if _, ok := canceledSourceDeletes.Load(id); ok {
		canceledSourceDeletes.Delete(id)
		deleteSourceDispatchMu.Unlock()
		syslog.Infof("[Collect] 异步删除站点已在排队中被取消，跳过清理 id=%s", id)
		return
	}
	activeDeletingSourceID.Store(&id)
	deleteSourceDispatchMu.Unlock()

	defer activeDeletingSourceID.Store(nil)
	defer progress.SetSourceDeleting(id, false)

	src := repository.FindCollectSourceForDelete(id)
	name := id
	if src != nil && src.Name != "" {
		name = src.Name
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
	deleteSourceDispatchMu.Lock()
	defer deleteSourceDispatchMu.Unlock()

	activePtr := activeDeletingSourceID.Load()
	activeID := ""
	if activePtr != nil {
		activeID = *activePtr
	}

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
		canceledSourceDeletes.Store(id, struct{}{})
		name := id
		if src := repository.FindCollectSourceById(id); src != nil && src.Name != "" {
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

func validateSourceForDelete(id string) (*model.FilmSource, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("采集站ID不能为空")
	}
	src := repository.FindCollectSourceForDelete(id)
	if src == nil {
		return nil, errors.New("当前资源站信息不存在, 请勿重复操作")
	}
	if progress.IsTaskRunning(id) {
		status := ""
		if snap, ok := progress.Snapshot(id); ok {
			status = snap.Status
		}
		syslog.Infof("[Collect] 拒绝删除，站点采集中 id=%s name=%s status=%s", id, src.Name, status)
		return nil, errors.New("采集中，暂不可删除")
	}
	if progress.IsSourceDeleting(id) {
		return nil, errors.New("该站正在删除")
	}
	if progress.IsCategorySyncing(id) {
		return nil, errors.New("分类采集中，暂不可删除")
	}
	return src, nil
}

type delSourceReservation struct {
	queue   chan string
	sources []*model.FilmSource
}

func reserveDelFilmSources(queue chan string, sources []*model.FilmSource) (*delSourceReservation, error) {
	deleteSourceEnqueueMu.Lock()
	defer deleteSourceEnqueueMu.Unlock()

	if cap(queue)-len(queue) < len(sources) {
		return nil, errors.New("删除队列已满")
	}
	for _, src := range sources {
		if progress.IsSourceDeleting(src.Id) {
			return nil, errors.New("该站正在删除")
		}
	}
	for _, src := range sources {
		progress.SetSourceDeleting(src.Id, true)
	}
	return &delSourceReservation{queue: queue, sources: sources}, nil
}

func (r *delSourceReservation) rollback() {
	if r == nil {
		return
	}
	deleteSourceEnqueueMu.Lock()
	defer deleteSourceEnqueueMu.Unlock()
	for _, src := range r.sources {
		progress.SetSourceDeleting(src.Id, false)
	}
}

func (r *delSourceReservation) commit(orderedSources []*model.FilmSource) {
	if r == nil {
		return
	}
	deleteSourceEnqueueMu.Lock()
	defer deleteSourceEnqueueMu.Unlock()

	sourcesToEnqueue := orderedSources
	if len(sourcesToEnqueue) == 0 {
		sourcesToEnqueue = r.sources
	}
	deleteSourceJobs.Add(len(sourcesToEnqueue))
	for _, src := range sourcesToEnqueue {
		progress.BroadcastSourceDeleteStatus(src.Id, src.Name, "deleting", "", 0)
		r.queue <- src.Id
	}
}

func enqueueDelFilmSources(queue chan string, sources []*model.FilmSource) error {
	res, err := reserveDelFilmSources(queue, sources)
	if err != nil {
		return err
	}
	res.commit(nil)
	return nil
}

func enqueueDelFilmSource(src *model.FilmSource) error {
	return enqueueDelFilmSources(deleteSourceQueue, []*model.FilmSource{src})
}

// TriggerDelFilmSource 异步触发采集站删除：广播 deleting 状态并毫秒级返回，后台串行队列安全停工与级联清理。
// 删除当前主站必须携带 newPrimaryID，或无其他启用站时 confirmClearPrimary 置空（D6）。
func (s *CollectService) TriggerDelFilmSource(id, newPrimaryID string, confirmClearPrimary bool) error {
	src, err := validateSourceForDelete(id)
	if err != nil {
		return err
	}
	// D6：删除当前主站，在准入锁内完成主站交接，消除并发切主与删除竞态
	return withPrimaryMutation(func(searchBusy bool) error {
		res, err := reserveDelFilmSources(deleteSourceQueue, []*model.FilmSource{src})
		if err != nil {
			return err
		}
		committed := false
		defer func() {
			if !committed {
				res.rollback()
			}
		}()

		oldPrimary := repository.PickPrimarySourceForCategory()
		if oldPrimary != nil && oldPrimary.Id == src.Id {
			if err := planPrimaryHandover(searchBusy, src.Id, newPrimaryID, confirmClearPrimary); err != nil {
				return err
			}
			if err := applyPrimaryHandover(oldPrimary, src.Id, newPrimaryID, confirmClearPrimary); err != nil {
				return err
			}
		}
		res.commit(nil)
		committed = true
		return nil
	})
}

// TriggerDelFilmSourceBatch 异步批量触发采集站删除。任一站校验不通过时整批拒绝，不触发部分删除。
// 批量包含当前主站时：将主站平滑交接并排在队列最后一位删除。
func (s *CollectService) TriggerDelFilmSourceBatch(ids []string, newPrimaryID string, confirmClearPrimary bool) error {
	if len(ids) == 1 {
		return s.TriggerDelFilmSource(ids[0], newPrimaryID, confirmClearPrimary)
	}
	return withPrimaryMutation(func(searchBusy bool) error {
		targets := make([]*model.FilmSource, 0, len(ids))
		seen := make(map[string]struct{}, len(ids))
		var failed []string
		for _, raw := range ids {
			id := strings.TrimSpace(raw)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			src, err := validateSourceForDelete(id)
			if err != nil {
				failed = append(failed, err.Error())
				continue
			}
			targets = append(targets, src)
		}
		if len(failed) > 0 {
			return errors.New(strings.Join(failed, "；"))
		}
		if len(targets) == 0 {
			return errors.New("采集站ID不能为空")
		}
		res, err := reserveDelFilmSources(deleteSourceQueue, targets)
		if err != nil {
			return err
		}
		committed := false
		defer func() {
			if !committed {
				res.rollback()
			}
		}()

		oldPrimary := repository.PickPrimarySourceForCategory()
		var primaryTarget *model.FilmSource
		if oldPrimary != nil {
			for _, target := range targets {
				if target.Id == oldPrimary.Id {
					primaryTarget = target
					break
				}
			}
		}

		if primaryTarget != nil {
			otherEnabled := repository.FindOtherEnabledCollectSourcesExcluding(seen)
			targetNewPrimary := strings.TrimSpace(newPrimaryID)
			if targetNewPrimary != "" {
				if err := planPrimaryHandoverExcluding(searchBusy, seen, primaryTarget.Id, targetNewPrimary, false); err != nil {
					return err
				}
				if err := applyPrimaryHandover(oldPrimary, primaryTarget.Id, targetNewPrimary, false); err != nil {
					return err
				}
			} else if len(otherEnabled) > 0 {
				return errors.New("仍有其他启用站，请指定新主站")
			} else {
				if err := planPrimaryHandoverExcluding(searchBusy, seen, primaryTarget.Id, "", confirmClearPrimary); err != nil {
					return err
				}
				if err := applyPrimaryHandover(oldPrimary, primaryTarget.Id, "", confirmClearPrimary); err != nil {
					return err
				}
			}

			// 核心：非主站先删，当前主站排在队列最后一位删除
			nonPrimaryTargets := make([]*model.FilmSource, 0, len(targets))
			for _, target := range targets {
				if target.Id != primaryTarget.Id {
					nonPrimaryTargets = append(nonPrimaryTargets, target)
				}
			}
			targets = append(nonPrimaryTargets, primaryTarget)
		}

		res.commit(targets)
		committed = true
		return nil
	})
}

func (s *CollectService) DelFilmSource(id string) (int, error) {
	return s.delFilmSource(id)
}

func (s *CollectService) delFilmSource(id string) (int, error) {
	if !progress.IsSourceDeleting(id) {
		progress.SetSourceDeleting(id, true)
		defer progress.SetSourceDeleting(id, false)
	}
	if err := waitDeleteIdle(id, deleteIndexWait); err != nil {
		return 0, err
	}
	deletedCount := 0
	err := spider.WithSourceExclusive(id, func() error {
		src := repository.FindCollectSourceById(id)
		if src == nil {
			return errors.New("当前资源站信息不存在, 请勿重复操作")
		}
		if progress.IsTaskRunning(id) {
			status := ""
			if snap, ok := progress.Snapshot(id); ok {
				status = snap.Status
			}
			syslog.Infof("[Collect] 删除执行时站点已在采集中，放弃 id=%s name=%s status=%s", id, src.Name, status)
			return errors.New("采集中，暂不可删除")
		}
		release, err := spider.StopSourceForDelete(id)
		if err != nil {
			return err
		}
		defer release()
		var orphanMids []int64
		err = spider.PauseActiveBatchRecords(id, func() error {
			var purgeErr error
			orphanMids, purgeErr = repository.DelCollectResource(id)
			return purgeErr
		})
		if err != nil {
			if len(orphanMids) > 0 {
				filmsnapshot.InvalidateCachesAfterSourceDelete(id)
			}
			return err
		}
		deletedCount = len(orphanMids)
		filmsnapshot.InvalidateCachesAfterSourceDelete(id)
		if len(orphanMids) > 0 {
			syslog.Infof("[Collect] 删除采集源 %s(%s)，已删除该站档案 %d 部", src.Name, id, len(orphanMids))
		}
		spider.ClearLimiter(id)
		clearProvideNetworkConfigCache()
		repository.MarkCategoryChanged()
		progress.BroadcastCategoryStatus(id, "idle")
		return nil
	})
	if err != nil {
		return 0, err
	}
	spider.DropSourceRuntime(id)
	return deletedCount, nil
}
