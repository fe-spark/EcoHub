package service

import (
	"errors"
	"strings"

	"server/internal/infra/syslog"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider"
	"server/internal/spider/progress"
)

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
	progress.BroadcastSourceDeleteStatus(id, name, "deleting", "", 0)
	deletedFilms, err := CollectSvc.DelFilmSource(id)
	if err != nil {
		syslog.Errorf("[Collect] 异步删除站点 %s 失败: %v", id, err)
		progress.BroadcastSourceDeleteStatus(id, name, "failed", err.Error(), 0)
		return
	}
	progress.BroadcastSourceDeleteStatus(id, name, "deleted", "", deletedFilms)
}

// TriggerDelFilmSource 异步触发采集站删除：广播 deleting 状态并毫秒级返回，后台串行队列安全停工与级联清理
func (s *CollectService) TriggerDelFilmSource(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("采集站ID不能为空")
	}
	if IsPrimarySiteSwitching() {
		return errors.New("检索索引正在构建中，暂不可操作站点，请稍候")
	}
	src := repository.FindCollectSourceForDelete(id)
	if src == nil {
		return errors.New("当前资源站信息不存在, 请勿重复操作")
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
	deletedCount := 0
	err := spider.WithSourceExclusive(id, func() error {
		src := repository.FindCollectSourceById(id)
		if src == nil {
			return errors.New("当前资源站信息不存在, 请勿重复操作")
		}
		// 采集中、停止中、分类采集中都不拦截。先停采集，等本次运行退出后再删。
		release, err := spider.StopSourceForDelete(id)
		if err != nil {
			return err
		}
		defer release()
		oldPrimary := repository.PickPrimarySourceForCategory()
		orphanMids, err := repository.DelCollectResource(id)
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
