package service

import (
	"errors"
	"fmt"
	"server/internal/access"
	"time"

	"server/internal/config"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider"
)

// BackupService 站点配置备份/恢复（不含影视库存与账号）
type BackupService struct{}

var (
	BackupSvc                      = new(BackupService)
	syncImportedSourceCategoriesFn = func() {
		go func() {
			if err := SpiderSvc.SyncAllSourceCategories(); err != nil {
				syslog.Errorf("[Backup] 导入采集站后自动同步分类树失败: %v", err)
			}
		}()
	}
)

// ExportConfig 导出当前可迁移配置
func (s *BackupService) ExportConfig() (model.ConfigBackup, error) {
	sources, err := repository.GetCollectSourceList()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	if sources == nil {
		sources = []model.FilmSource{}
	}
	banners, err := repository.GetBanners()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	if banners == nil {
		banners = model.Banners{}
	}
	bannerConfig, err := repository.GetBannerConfig()
	if err != nil {
		return model.ConfigBackup{}, err
	}

	site, err := repository.GetSiteBasic()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	proxyCfg, err := repository.GetProxyConfig()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	tmdbCfg, err := repository.GetTMDBConfig()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	notifyCfg, err := repository.GetNotifyConfig()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	analyticsCfg, err := repository.GetAnalyticsConfig()
	if err != nil {
		return model.ConfigBackup{}, err
	}

	rules, err := repository.ListAllMappingRules(repository.MappingRuleQuery{})
	if err != nil {
		return model.ConfigBackup{}, err
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

	return model.ConfigBackup{
		Version:      model.ConfigBackupVersion,
		ExportedAt:   time.Now().Format(time.RFC3339),
		AppVersion:   config.Version,
		Site:         &site,
		FilmSources:  sources,
		Banners:      banners,
		BannerConfig: &bannerConfig,
		MappingRules: exports,
		Proxy:        &proxyCfg,
		TMDB:         &tmdbCfg,
		Notify:       &notifyCfg,
		Analytics:    &analyticsCfg,
	}, nil
}

// ImportConfig 按模块恢复配置备份（调用方已校验密码）
func (s *BackupService) ImportConfig(req model.ConfigBackupImportRequest) error {
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

	// 2. 导入采集站前停掉采集。未开写的页直接丢掉，只等正在写的事务。
	if req.Modules.FilmSources {
		if err := spider.StopAllTasksAndWait(0, "导入采集站配置，停止采集"); err != nil {
			return fmt.Errorf("停止进行中的采集失败: %w", err)
		}
	}

	needCategoryRefresh := false

	if req.Modules.Site {
		if err := repository.SaveSiteBasic(*backup.Site); err != nil {
			return fmt.Errorf("导入网站配置失败: %w", err)
		}
	}

	if req.Modules.FilmSources {
		list := backup.FilmSources
		// 导入前清理旧站限流器
		oldSources, err := repository.GetCollectSourceList()
		if err != nil {
			return err
		}
		for _, src := range oldSources {
			spider.ClearLimiter(src.Id)
		}
		if err := repository.ReplaceCollectSources(list); err != nil {
			return fmt.Errorf("导入采集站失败: %w", err)
		}
		for _, src := range list {
			spider.ClearLimiter(src.Id)
		}
		clearProvideNetworkConfigCache()
		repository.ClearIndexPageCache()
		needCategoryRefresh = true
	}

	if req.Modules.Banners {
		banners := backup.Banners
		if err := repository.SaveBanners(banners); err != nil {
			return fmt.Errorf("导入首页轮播失败: %w", err)
		}
		if backup.BannerConfig != nil {
			if err := repository.SaveBannerConfig(*backup.BannerConfig); err != nil {
				return fmt.Errorf("导入首页轮播排片配置失败: %w", err)
			}
		}
	}

	if req.Modules.MappingRules {
		exports := backup.MappingRules
		if err := repository.ReplaceMappingRules(exports); err != nil {
			return fmt.Errorf("导入映射规则失败: %w", err)
		}
		needCategoryRefresh = true
	}

	if req.Modules.Proxy {
		if err := repository.SaveProxyConfig(*backup.Proxy); err != nil {
			return fmt.Errorf("导入网络代理配置失败: %w", err)
		}
		NotifySvc.EnsureBotPoller()
	}

	if req.Modules.TMDB {
		if err := repository.SaveTMDBConfig(*backup.TMDB); err != nil {
			return fmt.Errorf("导入 TMDB 刮削配置失败: %w", err)
		}
	}

	if req.Modules.Notify {
		if err := repository.SaveNotifyConfig(*backup.Notify); err != nil {
			return fmt.Errorf("导入通知配置失败: %w", err)
		}
		NotifySvc.EnsureBotPoller()
	}

	if needCategoryRefresh {
		if err := repository.RefreshFutureCategoryMappingsFromSourceCategories(); err != nil {
			syslog.Errorf("[Backup] 刷新来源分类映射失败: %v", err)
		}
		if err := filmsnapshot.RefreshActiveProjectedReadModel(); err != nil {
			syslog.Errorf("[Backup] 刷新读模型投影失败: %v", err)
		}
	}
	if req.Modules.Analytics {
		if err := repository.SaveAnalyticsConfig(*backup.Analytics); err != nil {
			return fmt.Errorf("导入数据分析配置失败: %w", err)
		}
		access.SetEnabled(backup.Analytics.Enabled)
	}

	if req.Modules.FilmSources {
		syncImportedSourceCategoriesFn()
	}

	return nil
}

func validateImportPayload(req model.ConfigBackupImportRequest) error {
	b := req.Backup
	if req.Modules.Site && b.Site == nil {
		return errors.New("备份中缺少网站配置数据")
	}
	if req.Modules.FilmSources && b.FilmSources == nil {
		return errors.New("备份中缺少采集站")
	}
	if req.Modules.Banners && b.Banners == nil {
		return errors.New("备份中缺少首页轮播")
	}
	if req.Modules.MappingRules && b.MappingRules == nil {
		return errors.New("备份中缺少映射规则")
	}
	if req.Modules.Proxy && b.Proxy == nil {
		return errors.New("备份中缺少网络代理配置数据")
	}
	if req.Modules.TMDB && b.TMDB == nil {
		return errors.New("备份中缺少 TMDB 刮削配置数据")
	}
	if req.Modules.Notify && b.Notify == nil {
		return errors.New("备份中缺少通知配置数据")
	}
	if req.Modules.Analytics && b.Analytics == nil {
		return errors.New("备份中缺少数据分析配置数据")
	}
	return nil
}
