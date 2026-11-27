package backup

import (
	"context"
	"errors"
	"fmt"
	"server/internal/access"
	"server/internal/admin/upgrade"
	"server/internal/banner"
	"server/internal/collect"
	"server/internal/collectctl"
	"server/internal/config"
	filmrepo "server/internal/film"
	"server/internal/film/catalog"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/infra/syslog"
	"server/internal/mapping"
	"server/internal/mediaproxy"
	"server/internal/model"
	"server/internal/notify"
	"server/internal/proxy"
	"server/internal/repobase"
	"server/internal/siteops"
	"server/internal/sourcecatalog"
	"server/internal/tmdb"
	"server/internal/utils"
	"time"
)

// BackupService 站点配置备份/恢复（不含影视库存与账号）
type BackupService struct{}

var (
	BackupSvc                      = new(BackupService)
	syncImportedSourceCategoriesFn = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := collect.SpiderSvc.SyncAllSourceCategories(ctx); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				syslog.Warnf("[Backup] 导入后自动同步分类树超时，已取消剩余同步")
			} else {
				syslog.Errorf("[Backup] 导入采集站后自动同步分类树失败: %v", err)
			}
		}
	}
)

// ExportConfig 导出当前可迁移配置
func (s *BackupService) ExportConfig() (model.ConfigBackup, error) {
	sources, err := collect.GetCollectSourceList()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	if sources == nil {
		sources = []model.FilmSource{}
	}
	bannerSlots, err := banner.ListAllBannerSlots()
	if err != nil {
		return model.ConfigBackup{}, err
	}

	site, err := repobase.GetSiteBasic()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	proxyCfg, err := repobase.GetProxyConfig()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	tmdbCfg, err := tmdb.GetTMDBConfig()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	notifyCfg, err := notify.GetNotifyConfig()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	analyticsCfg, err := access.GetAnalyticsConfig()
	if err != nil {
		return model.ConfigBackup{}, err
	}

	exports, err := exportMappingRuleItems()
	if err != nil {
		return model.ConfigBackup{}, err
	}

	return model.ConfigBackup{
		Version:      model.ConfigBackupVersion,
		ExportedAt:   time.Now().Format(time.RFC3339),
		AppVersion:   config.Version,
		Site:         &site,
		FilmSources:  sources,
		BannerSlots:  bannerSlots,
		MappingRules: exports,
		Proxy:        &proxyCfg,
		TMDB:         &tmdbCfg,
		Notify:       &notifyCfg,
		Analytics:    &analyticsCfg,
	}, nil
}

// ImportConfig 按模块恢复配置备份（调用方已校验密码）
func (s *BackupService) ImportConfig(req model.ConfigBackupImportRequest) (retErr error) {
	if !req.Modules.Any() {
		return errors.New("请至少选择一个要导入的模块")
	}
	backup := req.Backup
	if backup.Version != 0 && backup.Version != model.ConfigBackupVersion {
		return fmt.Errorf("不支持的备份版本：%d（当前支持 %d）", backup.Version, model.ConfigBackupVersion)
	}

	// 1. 动库前统一校验所有勾选模块的数据是否存在，避免部分模块生效后才报错
	if err := validateImportPayload(req); err != nil {
		return err
	}

	// 2. 与站点重置及并发导入严格互斥，导入采集站或映射规则前先停掉采集与排队删站。
	if siteops.ResetSvc.SiteResetRunning() {
		return errors.New("站点数据正在重置中，请稍后再导入配置")
	}
	unlockMaint, ok := siteops.TryLockSiteMaintenance()
	if !ok {
		return errors.New("正在执行站点维护操作（重置或配置导入），请稍后再试")
	}
	defer unlockMaint()
	if siteops.ResetSvc.SiteResetRunning() {
		return errors.New("站点数据正在重置中，请稍后再导入配置")
	}
	var oldPrimaryBeforeImport *model.FilmSource
	if req.Modules.FilmSources || req.Modules.MappingRules {
		releaseGate, err := collect.BeginMaintenanceGate()
		if err != nil {
			return fmt.Errorf("进入采集维护态失败: %w", err)
		}
		defer releaseGate()
		if err := collect.StopAllTasksAndWait(0, "导入配置，停止采集"); err != nil {
			return fmt.Errorf("停止进行中的采集失败: %w", err)
		}
		collectctl.CollectSvc.CancelDelFilmSources(nil)
		collectctl.WaitDeleteSourceJobs()
		collect.WaitCategorySyncs()
		oldPrimaryBeforeImport = sourcecatalog.CurrentPrimarySource()
	}

	needCategoryRefresh := false
	primaryChanged := false
	filmSourcesCommitted := false
	mappingRulesCommitted := false
	defer func() {
		if needCategoryRefresh {
			if err := catalog.RefreshFutureCategoryMappingsFromSourceCategories(); err != nil {
				syslog.Errorf("[Backup] 刷新来源分类映射失败: %v", err)
			}
			// applyPrimaryChanged 已执行过 RefreshAccessDataCaches，避免重复扫删 Redis 前缀
			if !primaryChanged {
				if err := filmsnapshot.RefreshActiveProjectedReadModel(); err != nil {
					syslog.Errorf("[Backup] 刷新读模型投影失败: %v", err)
				}
			}
		}
		if filmSourcesCommitted {
			syncImportedSourceCategoriesFn()
		}
		if filmSourcesCommitted || mappingRulesCommitted {
			filmrepo.RefreshInventoryStatsAsync()
		}
	}()

	if req.Modules.Site {
		if err := repobase.SaveSiteBasic(*backup.Site); err != nil {
			return fmt.Errorf("导入网站配置失败: %w", err)
		}
		mediaproxy.InvalidateMediaProxyMemoryCache()
		if !req.Modules.FilmSources && !req.Modules.MappingRules {
			filmsnapshot.BumpSearchCacheVersion()
		}
	}

	if req.Modules.FilmSources {
		list := backup.FilmSources
		var replaceErr error
		if err := filmsnapshot.WithPrimaryMutation(func(_ bool) error {
			oldSources, err := collect.GetCollectSourceList()
			if err != nil {
				return err
			}
			for _, src := range oldSources {
				collect.ResetSourceThrottle(src.Id)
			}
			if err := collect.ReplaceCollectSources(list); err != nil {
				return fmt.Errorf("导入采集站失败: %w", err)
			}
			for _, src := range list {
				collect.ResetSourceThrottle(src.Id)
			}
			return nil
		}); err != nil {
			replaceErr = err
		}
		if replaceErr != nil {
			return replaceErr
		}
		primaryChanged = collectctl.ApplyPrimaryChanged(oldPrimaryBeforeImport, sourcecatalog.CurrentPrimarySource())
		if !primaryChanged {
			repobase.ClearIndexPageCache()
			filmsnapshot.BumpSearchCacheVersion()
		}
		repobase.ClearProvideNetworkConfigCache()
		filmsnapshot.ClearDynamicPlayCaches()
		filmsnapshot.ClearDerivedFilmListCaches()
		needCategoryRefresh = true
		filmSourcesCommitted = true
	}

	if req.Modules.Banners {
		slots, err := importBannerSlots(backup)
		if err != nil {
			return fmt.Errorf("导入首页轮播失败: %w", err)
		}
		if err := banner.ReplaceAllBannerSlots(slots); err != nil {
			return fmt.Errorf("导入首页轮播失败: %w", err)
		}
	}

	if req.Modules.MappingRules {
		if err := mapping.ReplaceMappingRules(backup.MappingRules); err != nil {
			return fmt.Errorf("导入映射规则失败: %w", err)
		}
		needCategoryRefresh = true
		mappingRulesCommitted = true
	}

	if req.Modules.Proxy {
		proxyCfg, err := proxy.ValidateAndNormalizeProxyConfig(*backup.Proxy)
		if err != nil {
			return fmt.Errorf("导入网络代理配置失败: %w", err)
		}
		if err := repobase.SaveProxyConfig(proxyCfg); err != nil {
			return fmt.Errorf("导入网络代理配置失败: %w", err)
		}
		utils.ResetProxyTransports()
		notify.NotifySvc.EnsureBotPoller()
		upgrade.TriggerReleasePoll()
	}

	if req.Modules.TMDB {
		if err := tmdb.SaveTMDBConfig(*backup.TMDB); err != nil {
			return fmt.Errorf("导入 TMDB 刮削配置失败: %w", err)
		}
	}

	if req.Modules.Notify {
		notifyCfg, err := notify.ValidateNotifyConfig(*backup.Notify)
		if err != nil {
			return fmt.Errorf("导入通知配置失败: %w", err)
		}
		if err := notify.SaveNotifyConfig(notifyCfg); err != nil {
			return fmt.Errorf("导入通知配置失败: %w", err)
		}
		notify.NotifySvc.EnsureBotPoller()
	}

	if req.Modules.Analytics {
		if err := access.SaveAnalyticsConfig(*backup.Analytics); err != nil {
			return fmt.Errorf("导入数据分析配置失败: %w", err)
		}
		access.SetEnabled(backup.Analytics.Enabled)
	}

	return nil
}

func exportMappingRuleItems() ([]model.MappingRuleExport, error) {
	rules, err := mapping.ListAllMappingRules(mapping.MappingRuleQuery{})
	if err != nil {
		return nil, err
	}
	exports := make([]model.MappingRuleExport, 0, len(rules))
	for _, r := range rules {
		exports = append(exports, model.MappingRuleExport{
			SourceId:  r.SourceId,
			Group:     r.Group,
			Raw:       r.Raw,
			Target:    r.Target,
			MatchType: r.MatchType,
			Remarks:   r.Remarks,
		})
	}
	return exports, nil
}
