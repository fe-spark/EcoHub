package service

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
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

func enabledSources(sources []model.FilmSource) []model.FilmSource {
	out := make([]model.FilmSource, 0, len(sources))
	for _, src := range sources {
		if src.State {
			out = append(out, src)
		}
	}
	return out
}

func sourceIDOrder(sources []model.FilmSource) []string {
	out := make([]string, 0, len(sources))
	for _, src := range sources {
		if id := strings.TrimSpace(src.Id); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func (s *CollectService) SortFilmSources(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	// 主站必须始终位于首位，排序仅允许调整非主站的顺序；仍与索引重建共用准入锁。
	return withPrimarySwitchAdmitted(func() error {
		primary := repository.CurrentPrimarySource()
		if primary != nil && strings.TrimSpace(ids[0]) != primary.Id {
			return errors.New("当前主站必须保持在首位")
		}
		beforeList, err := repository.GetCollectSourceList()
		if err != nil {
			return err
		}
		if err := repository.SortCollectSources(ids); err != nil {
			return err
		}
		filmsnapshot.ClearDynamicPlayCaches()
		clearProvideNetworkConfigCache()

		afterList, err := repository.GetCollectSourceList()
		if err == nil {
			afterEnabled := enabledSources(afterList)
			if !slices.Equal(sourceIDOrder(enabledSources(beforeList)), sourceIDOrder(afterEnabled)) {
				NotifySvc.PublishSourceOrderChange(afterEnabled)
			}
		}
		return nil
	})
}

// UpdateFilmSource 编辑采集源配置（单源）。
// 采集链接变更时先在主站准入锁内完成全部校验，再释放全局锁于单站独占锁内执行分批清库，最后重新加锁提交配置与主站交接。
// 停用当前主站必须携带 newPrimaryID，或无其他启用站时 confirmClearPrimary 置空（D6）。
func (s *CollectService) UpdateFilmSource(source model.FilmSource, newPrimaryID string, confirmClearPrimary bool) error {
	return s.UpdateFilmSourceWithAuth(source, newPrimaryID, confirmClearPrimary, true)
}

// UpdateFilmSourceWithAuth 编辑采集源配置并校验主站交接操作权限（D4）。
func (s *CollectService) UpdateFilmSourceWithAuth(source model.FilmSource, newPrimaryID string, confirmClearPrimary, allowPrimaryHandover bool) error {
	if !allowPrimaryHandover && (strings.TrimSpace(newPrimaryID) != "" || confirmClearPrimary) {
		return ErrPrimarySwitchForbidden
	}
	return spider.WithSourceExclusive(source.Id, func() error {
		var orphanMids []int64
		cleaned := false
		var err error
		defer func() {
			if len(orphanMids) > 0 || (err != nil && cleaned) {
				filmsnapshot.InvalidateCachesAfterSourceDelete(source.Id)
			}
		}()

		var needClean bool
		err = withPrimaryMutation(func(searchBusy bool) error {
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
			if isUriChanged {
				if err := repository.ValidateCollectSourceUpdate(source); err != nil {
					return err
				}
			}
			oldPrimary := repository.CurrentPrimarySource()
			// 主站换地址会清空其数据并牵动检索索引，重建忙碌时拒绝
			if searchBusy && isUriChanged && oldPrimary != nil && oldPrimary.Id == source.Id {
				return filmsnapshot.ErrSearchIndexBusy
			}
			// D6：停用当前主站必须同时完成主站交接（仅超管可执行，D4）
			disablingPrimary := oldPrimary != nil && oldPrimary.Id == source.Id && latest.State && !source.State
			if disablingPrimary {
				if !allowPrimaryHandover {
					return ErrPrimarySwitchForbidden
				}
				if err := planPrimaryHandover(searchBusy, source.Id, newPrimaryID, confirmClearPrimary); err != nil {
					return err
				}
			}
			if isUriChanged {
				needClean = true
				return nil
			}
			if err := s.updateFilmSourceCore(source, false, 0); err != nil {
				return err
			}
			if disablingPrimary {
				if err := applyPrimaryHandover(oldPrimary, source.Id, newPrimaryID, confirmClearPrimary); err != nil {
					return err
				}
			} else if oldPrimary == nil && source.State {
				// D5：主站为空时，被启用的站经懒初始化自动成为主站
				applyPrimaryChanged(oldPrimary, repository.CurrentPrimarySource())
			}
			return nil
		})
		if err != nil || !needClean {
			return err
		}

		// 耗时分批清库在全局主站锁外执行（受单站独占锁 WithSourceExclusive 保护），避免阻塞全站写操作
		cleaned = true
		cleaningURISources.Store(source.Id, struct{}{})
		defer cleaningURISources.Delete(source.Id)
		var cleanErr error
		orphanMids, cleanErr = repository.CleanCollectSourceData(source.Id)
		if cleanErr != nil {
			err = cleanErr
			return err
		}

		err = withPrimaryMutation(func(searchBusy bool) error {
			latest := repository.FindCollectSourceById(source.Id)
			if latest == nil {
				return errors.New("采集站信息不存在")
			}
			if progress.IsSourceDeleting(source.Id) {
				return errors.New("该站正在删除")
			}
			if latest.State != source.State {
				if reason := sourceBusyReason(source.Id); reason != "" {
					return errors.New(reason)
				}
			}
			oldPrimary := repository.CurrentPrimarySource()
			disablingPrimary := oldPrimary != nil && oldPrimary.Id == source.Id && latest.State && !source.State
			if disablingPrimary {
				if !allowPrimaryHandover {
					return ErrPrimarySwitchForbidden
				}
				if err := planPrimaryHandover(searchBusy, source.Id, newPrimaryID, confirmClearPrimary); err != nil {
					return err
				}
			}
			if err := s.updateFilmSourceCore(source, true, len(orphanMids)); err != nil {
				return err
			}
			if disablingPrimary {
				if err := applyPrimaryHandover(oldPrimary, source.Id, newPrimaryID, confirmClearPrimary); err != nil {
					return err
				}
			} else if oldPrimary == nil && source.State {
				applyPrimaryChanged(oldPrimary, repository.CurrentPrimarySource())
			}
			return nil
		})
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
	spider.ClearCMSSearchUnsupported(old.Uri)
	spider.ClearCMSSearchUnsupported(source.Uri)
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
	filmrepo.RefreshInventoryStatsAsync()
	return nil
}

func (s *CollectService) BatchUpdateFilmSourceState(ids []string, state bool) error {
	if len(ids) == 0 {
		return nil
	}
	return withPrimaryMutation(func(searchBusy bool) error {
		oldPrimary := repository.CurrentPrimarySource()
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
			applyPrimaryChanged(oldPrimary, repository.CurrentPrimarySource())
		}
		filmrepo.RefreshInventoryStatsAsync()
		return nil
	})
}

func (s *CollectService) SaveFilmSource(source model.FilmSource) error {
	return withPrimaryMutation(func(searchBusy bool) error {
		if source.Id != "" && progress.IsSourceDeleting(source.Id) {
			return errors.New("该站正在删除")
		}
		primaryWasEmpty := repository.CurrentPrimarySource() == nil
		if source.Id == "" {
			source.Id = utils.GenerateHashKey(source.Uri)
		}
		if err := repository.AddCollectSource(source); err != nil {
			return err
		}
		spider.ClearLimiter(source.Id)
		spider.ClearCMSSearchUnsupported(source.Uri)
		clearProvideNetworkConfigCache()
		filmsnapshot.ClearDynamicPlayCaches()
		if primaryWasEmpty && source.State {
			// D5：主站为空时新增的启用站自动成为主站
			applyPrimaryChanged(nil, repository.CurrentPrimarySource())
		}
		filmrepo.RefreshInventoryStatsAsync()
		return nil
	})
}
