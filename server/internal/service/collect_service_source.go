package service

import (
	"errors"
	"fmt"
	"strings"

	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider"
	"server/internal/spider/progress"
	"server/internal/utils"
)

func withPrimarySwitchAdmitted(fn func() error) error {
	return filmsnapshot.WithPrimaryAdmitted(func() error {
		if primaryWarmCount.Load() > 0 {
			return filmsnapshot.ErrSearchIndexBusy
		}
		return fn()
	})
}

func withPrimaryMutation(fn func(searchBusy bool) error) error {
	return filmsnapshot.WithPrimarySwitchLock(func(searchBusy bool) error {
		return fn(searchBusy || primaryWarmCount.Load() > 0)
	})
}

func (s *CollectService) SortFilmSources(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	// 排序只改顺序，不再切换主站；仍与索引重建共用锁，避免重建途中排序产生竞态。
	return withPrimarySwitchAdmitted(func() error {
		if err := repository.SortCollectSources(ids); err != nil {
			return err
		}
		filmsnapshot.ClearDynamicPlayCaches()
		clearProvideNetworkConfigCache()
		return nil
	})
}

// UpdateFilmSource 编辑采集源配置（单源）。
// 采集链接变更时在单站独占锁与主站准入锁保护下先校验再分批清空该站数据并更新分类和配置，避免清库后配置更新被拒绝导致状态不一致。
// 停用当前主站必须携带 newPrimaryID，或无其他启用站时 confirmClearPrimary 置空（D6）。
func (s *CollectService) UpdateFilmSource(source model.FilmSource, newPrimaryID string, confirmClearPrimary bool) error {
	return spider.WithSourceExclusive(source.Id, func() error {
		var orphanMids []int64
		cleaned := false
		err := withPrimaryMutation(func(searchBusy bool) error {
			latest := repository.FindCollectSourceById(source.Id)
			if latest == nil {
				return errors.New("采集站信息不存在")
			}
			if progress.IsSourceDeleting(source.Id) {
				return errors.New("该站正在删除")
			}
			if err := checkSourceEndpointMutable(latest, source); err != nil {
				return err
			}
			if latest.State != source.State {
				if reason := sourceBusyReason(source.Id); reason != "" {
					return errors.New(reason)
				}
			}
			isUriChanged := latest.Uri != source.Uri
			oldPrimary := repository.PickPrimarySourceForCategory()
			// 主站换地址会清空其数据并牵动检索索引，重建忙碌时拒绝
			if searchBusy && isUriChanged && oldPrimary != nil && oldPrimary.Id == source.Id {
				return filmsnapshot.ErrSearchIndexBusy
			}
			// D6：停用当前主站必须同时完成主站交接
			disablingPrimary := oldPrimary != nil && oldPrimary.Id == source.Id && latest.State && !source.State
			if disablingPrimary {
				if err := planPrimaryHandover(searchBusy, source.Id, newPrimaryID, confirmClearPrimary); err != nil {
					return err
				}
			}
			if isUriChanged {
				cleaned = true
				var cleanErr error
				orphanMids, cleanErr = repository.CleanCollectSourceData(source.Id)
				if cleanErr != nil {
					return cleanErr
				}
			}
			if err := s.updateFilmSourceCore(source, isUriChanged, len(orphanMids)); err != nil {
				return err
			}
			if disablingPrimary {
				if err := applyPrimaryHandover(oldPrimary, source.Id, newPrimaryID, confirmClearPrimary); err != nil {
					return err
				}
			} else if oldPrimary == nil && source.State {
				// D5：主站为空时，被启用的站经懒初始化自动成为主站
				applyPrimaryChanged(oldPrimary, repository.PickPrimarySourceForCategory())
			}
			return nil
		})
		if len(orphanMids) > 0 || (err != nil && cleaned) {
			filmsnapshot.InvalidateCachesAfterSourceDelete(source.Id)
		}
		return err
	})
}

func checkSourceEndpointMutable(old *model.FilmSource, next model.FilmSource) error {
	// 拉页、停止中、待检索、分类采集中都不许改地址或协议。待检索时清空数据会和封口抢同一批影片。
	if old.Uri == next.Uri && old.ResolveFormat() == next.ResolveFormat() {
		return nil
	}
	name := strings.TrimSpace(next.Name)
	if name == "" {
		name = old.Name
	}
	if progress.IsCategorySyncing(next.Id) {
		return fmt.Errorf("采集站「%s」分类采集中，暂不可修改地址或协议", name)
	}
	if progress.IsSealing(next.Id) {
		return fmt.Errorf("采集站「%s」待检索，暂不可修改地址或协议", name)
	}
	if spider.IsTaskRunning(next.Id) {
		return fmt.Errorf("采集站「%s」当前有采集任务正在运行或正在停止退出中，请稍候几秒后再修改地址或协议", name)
	}
	return nil
}

func sourceBusyReason(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if progress.IsSourceDeleting(id) {
		return "该站正在删除"
	}
	if progress.IsCategorySyncing(id) {
		return "分类采集中"
	}
	if progress.IsStopping(id) {
		return "停止中"
	}
	if progress.IsSealing(id) {
		return "待检索"
	}
	if progress.IsTaskRunning(id) {
		return "采集中"
	}
	return ""
}

func (s *CollectService) updateFilmSourceCore(source model.FilmSource, clearCategories bool, deletedFilms int) error {
	old := repository.FindCollectSourceById(source.Id)
	if old == nil {
		return errors.New("采集站信息不存在")
	}
	if progress.IsSourceDeleting(source.Id) {
		return errors.New("该站正在删除")
	}
	if old.State != source.State {
		if reason := sourceBusyReason(source.Id); reason != "" {
			return errors.New(reason)
		}
	}
	if source.CreatedAt.IsZero() && !old.CreatedAt.IsZero() {
		source.CreatedAt = old.CreatedAt
	}
	// 排序只走排序接口。编辑请求不带 sort，避免把别的站的排序覆盖成 0。
	source.Sort = old.Sort

	if err := repository.UpdateCollectSourceReplacingData(source, clearCategories); err != nil {
		return err
	}

	if clearCategories {
		syslog.Infof("[Collect] 采集源 %s(%s) 更换地址，已清空该站采集数据和分类，删除档案 %d 部", source.Name, source.Id, deletedFilms)
		repository.MarkCategoryChanged()
		progress.BroadcastCategoryReady(source.Id, false)
	}

	spider.ClearLimiter(source.Id)
	if old.State && !source.State {
		spider.StopCollectSource(source.Id, "采集站已停用")
	}

	clearProvideNetworkConfigCache()
	filmsnapshot.ClearDynamicPlayCaches()
	if old.IsPosterSource != source.IsPosterSource {
		filmsnapshot.BumpSearchCacheVersion()
		filmsnapshot.ClearDerivedFilmListCaches()
		filmsnapshot.RefreshAccessDataCaches()
		filmsnapshot.ClearSearchCache()
	}
	return nil
}

func (s *CollectService) BatchUpdateFilmSourceState(ids []string, state bool) error {
	if len(ids) == 0 {
		return nil
	}
	return withPrimaryMutation(func(searchBusy bool) error {
		oldPrimary := repository.PickPrimarySourceForCategory()
		// D6：批量停用不允许包含当前主站，主站交接须走单站停用
		if !state && oldPrimary != nil {
			for _, id := range ids {
				if strings.TrimSpace(id) == oldPrimary.Id {
					return errors.New("批量停用包含当前主站，请单独停用并指定新主站")
				}
			}
		}
		idsToUpdate := make([]string, 0, len(ids))
		for _, id := range ids {
			source := repository.FindCollectSourceById(id)
			if source == nil {
				return errors.New("采集站信息不存在")
			}
			if reason := sourceBusyReason(id); reason != "" {
				if reason == "该站正在删除" {
					return errors.New(reason)
				}
				return fmt.Errorf("站点「%s」%s，暂不可批量操作", source.Name, reason)
			}
			if source.State != state {
				idsToUpdate = append(idsToUpdate, id)
			}
		}
		if len(idsToUpdate) == 0 {
			return nil
		}
		if err := repository.BatchUpdateCollectSourceState(idsToUpdate, state); err != nil {
			return err
		}
		for _, id := range idsToUpdate {
			spider.ClearLimiter(id)
			if !state {
				spider.StopCollectSource(id, "采集站已停用")
			}
		}
		clearProvideNetworkConfigCache()
		filmsnapshot.ClearDynamicPlayCaches()
		if len(idsToUpdate) > 0 && oldPrimary == nil && state {
			// D5：主站为空时，被启用的站经懒初始化自动成为主站
			applyPrimaryChanged(oldPrimary, repository.PickPrimarySourceForCategory())
		}
		return nil
	})
}

func (s *CollectService) SaveFilmSource(source model.FilmSource) error {
	return withPrimaryMutation(func(searchBusy bool) error {
		if source.Id != "" && progress.IsSourceDeleting(source.Id) {
			return errors.New("该站正在删除")
		}
		primaryWasEmpty := repository.PickPrimarySourceForCategory() == nil
		if source.Id == "" {
			source.Id = utils.GenerateHashKey(source.Uri)
		}
		if err := repository.AddCollectSource(source); err != nil {
			return err
		}
		spider.ClearLimiter(source.Id)
		clearProvideNetworkConfigCache()
		filmsnapshot.ClearDynamicPlayCaches()
		if primaryWasEmpty && source.State {
			// D5：主站为空时新增的启用站自动成为主站
			applyPrimaryChanged(nil, repository.PickPrimarySourceForCategory())
		}
		return nil
	})
}
