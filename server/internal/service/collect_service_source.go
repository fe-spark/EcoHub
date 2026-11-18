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
		if isPrimaryWarming.Load() {
			return filmsnapshot.ErrSearchIndexBusy
		}
		return fn()
	})
}

func ensurePrimarySwitchAdmitted() error {
	return withPrimarySwitchAdmitted(func() error { return nil })
}

func (s *CollectService) SortFilmSources(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	// 和重建开始共用一把锁：检查通过后才写排序，重建途中不能切主站。
	return withPrimarySwitchAdmitted(func() error {
		oldPrimary := repository.PickPrimarySourceForCategory()
		if err := repository.SortCollectSources(ids); err != nil {
			return err
		}
		filmsnapshot.ClearDynamicPlayCaches()
		clearProvideNetworkConfigCache()
		newPrimary := repository.PickPrimarySourceForCategory()
		if !onPrimarySourceMaybeChanged(oldPrimary, newPrimary) {
			enabled, err := repository.GetEnabledCollectSourceList()
			if err != nil {
				return err
			}
			NotifySvc.PublishSourceOrderChange(oldPrimary, newPrimary, enabled)
		}
		return nil
	})
}

// UpdateFilmSource 编辑采集源配置（单源）。
// 采集链接变更时在单站独占锁与主站准入锁保护下先校验再分批清空该站数据并更新分类和配置，避免清库后配置更新被拒绝导致状态不一致。
func (s *CollectService) UpdateFilmSource(source model.FilmSource) error {
	return spider.WithSourceExclusive(source.Id, func() error {
		var orphanMids []int64
		cleaned := false
		err := withPrimarySwitchAdmitted(func() error {
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
			onPrimarySourceMaybeChanged(oldPrimary, repository.PickPrimarySourceForCategory())
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
	// 排序只走排序接口。编辑请求不带 sort，不能写成 0 把别的站顶成主站。
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
	return withPrimarySwitchAdmitted(func() error {
		planned := make(map[string]bool)
		for _, id := range ids {
			source := repository.FindCollectSourceById(id)
			if reason := sourceBusyReason(id); reason != "" {
				if reason == "该站正在删除" || source == nil {
					return errors.New(reason)
				}
				return fmt.Errorf("站点「%s」%s，暂不可批量操作", source.Name, reason)
			}
			if source == nil {
				return errors.New("采集站信息不存在")
			}
			if source.State != state {
				planned[id] = state
			}
		}
		if len(planned) == 0 {
			return nil
		}
		oldPrimary := repository.PickPrimarySourceForCategory()
		var firstErr error
		changed := false
		for _, id := range ids {
			source := repository.FindCollectSourceById(id)
			if source == nil {
				firstErr = errors.New("采集站信息不存在")
				break
			}
			if source.State == state {
				continue
			}
			next := *source
			next.State = state
			if err := s.updateFilmSourceCore(next, false, 0); err != nil {
				firstErr = err
				break
			}
			changed = true
		}
		if changed {
			onPrimarySourceMaybeChanged(oldPrimary, repository.PickPrimarySourceForCategory())
		}
		return firstErr
	})
}

func (s *CollectService) SaveFilmSource(source model.FilmSource) error {
	return withPrimarySwitchAdmitted(func() error {
		if source.Id != "" && progress.IsSourceDeleting(source.Id) {
			return errors.New("该站正在删除")
		}
		oldPrimary := repository.PickPrimarySourceForCategory()
		if source.Id == "" {
			source.Id = utils.GenerateHashKey(source.Uri)
		}
		if err := repository.AddCollectSource(source); err != nil {
			return err
		}
		spider.ClearLimiter(source.Id)
		clearProvideNetworkConfigCache()
		filmsnapshot.ClearDynamicPlayCaches()
		onPrimarySourceMaybeChanged(oldPrimary, repository.PickPrimarySourceForCategory())
		return nil
	})
}
