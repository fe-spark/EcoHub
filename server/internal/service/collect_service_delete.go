package service

import (
	"errors"
	"strings"
	"time"

	"server/internal/infra/syslog"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider"
	"server/internal/spider/progress"
)

// deleteIndexWait 删除前等待已经在进行的检索索引、封口和主站预热。
const deleteIndexWait = 30 * time.Minute

func deleteIdleReason() string {
	reason := spider.SearchWorkBusyReason()
	if !isPrimaryWarming.Load() {
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

var deleteSourceQueue = make(chan string, 1000)

func init() {
	go runDeleteSourceWorker()
}

func runDeleteSourceWorker() {
	for id := range deleteSourceQueue {
		processAsyncDeleteSource(id)
	}
}

func processAsyncDeleteSource(id string) {
	defer progress.SetSourceDeleting(id, false)
	src := repository.FindCollectSourceForDelete(id)
	name := id
	if src != nil && src.Name != "" {
		name = src.Name
	}
	syslog.Infof("[Collect] 异步删除站点开始 id=%s name=%s", id, name)
	started := time.Now()
	progress.BroadcastSourceDeleteStatus(id, name, "deleting", "", 0)
	deletedFilms, err := CollectSvc.DelFilmSource(id)
	if err != nil {
		syslog.Errorf("[Collect] 异步删除站点失败 id=%s name=%s 耗时=%s err=%v", id, name, time.Since(started).Round(time.Millisecond), err)
		progress.BroadcastSourceDeleteStatus(id, name, "failed", err.Error(), 0)
		return
	}
	syslog.Infof("[Collect] 异步删除站点完成 id=%s name=%s 删除影片=%d 耗时=%s", id, name, deletedFilms, time.Since(started).Round(time.Millisecond))
	progress.BroadcastSourceDeleteStatus(id, name, "deleted", "", deletedFilms)
}

// TriggerDelFilmSource 异步触发采集站删除：广播 deleting 状态并毫秒级返回，后台串行队列安全停工与级联清理
func (s *CollectService) TriggerDelFilmSource(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("采集站ID不能为空")
	}
	src := repository.FindCollectSourceForDelete(id)
	if src == nil {
		return errors.New("当前资源站信息不存在, 请勿重复操作")
	}
	if progress.IsTaskRunning(id) {
		status := ""
		if snap, ok := progress.Snapshot(id); ok {
			status = snap.Status
		}
		syslog.Infof("[Collect] 拒绝删除，站点采集中 id=%s name=%s status=%s", id, src.Name, status)
		return errors.New("采集中，暂不可删除")
	}
	if progress.IsSourceDeleting(id) {
		return errors.New("该站正在删除")
	}

	select {
	case deleteSourceQueue <- id:
		progress.BroadcastSourceDeleteStatus(id, src.Name, "deleting", "", 0)
		return nil
	default:
		return errors.New("删除队列已满")
	}
}

// TriggerDelFilmSourceBatch 异步批量触发采集站删除。有一站失败就返回错误。
func (s *CollectService) TriggerDelFilmSourceBatch(ids []string) error {
	var failed []string
	for _, id := range ids {
		if err := s.TriggerDelFilmSource(id); err != nil {
			failed = append(failed, err.Error())
		}
	}
	if len(failed) > 0 {
		return errors.New(strings.Join(failed, "；"))
	}
	return nil
}

func (s *CollectService) DelFilmSource(id string) (int, error) {
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
		oldPrimary := repository.PickPrimarySourceForCategory()
		var orphanMids []int64
		err = spider.PauseActiveBatchRecords(id, func() error {
			var purgeErr error
			orphanMids, purgeErr = repository.DelCollectResource(id)
			return purgeErr
		})
		if err != nil {
			if len(orphanMids) > 0 {
				filmsnapshot.DeleteActiveSnapshotsByMids(orphanMids...)
			}
			return err
		}
		deletedCount = len(orphanMids)
		if len(orphanMids) > 0 {
			filmsnapshot.DeleteActiveSnapshotsByMids(orphanMids...)
			syslog.Infof("[Collect] 删除采集源 %s(%s)，已删除该站档案 %d 部", src.Name, id, len(orphanMids))
		}
		spider.ClearLimiter(id)
		clearProvideNetworkConfigCache()
		filmsnapshot.ClearDynamicPlayCaches()
		repository.MarkCategoryChanged()
		progress.BroadcastCategoryStatus(id, "idle")
		onPrimarySourceMaybeChanged(oldPrimary, repository.PickPrimarySourceForCategory())
		return nil
	})
	if err != nil {
		return 0, err
	}
	spider.DropSourceRuntime(id)
	return deletedCount, nil
}
