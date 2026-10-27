package service

import (
	"errors"
	"fmt"
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

var BackupSvc = new(BackupService)

// ExportConfig 导出当前可迁移配置
func (s *BackupService) ExportConfig() (model.ConfigBackup, error) {
	sources, err := repository.GetCollectSourceList()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	if sources == nil {
		sources = []model.FilmSource{}
	}
	tasks, err := repository.GetAllFilmTask()
	if err != nil {
		return model.ConfigBackup{}, err
	}
	if tasks == nil {
		tasks = []model.FilmCollectTask{}
	} else {
		for i := range tasks {
			tasks[i].Cid = 0
		}
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

	rules, err := repository.ListAllMappingRules(repository.MappingRuleQuery{})
	if err != nil {
		return model.ConfigBackup{}, err
	}
	exports := make([]model.MappingRuleExport, 0, len(rules))
	for _, r := range rules {
		exports = append(exports, model.MappingRuleExport{
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
		CronTasks:    tasks,
		Banners:      banners,
		BannerConfig: &bannerConfig,
		MappingRules: exports,
		Proxy:        &proxyCfg,
		TMDB:         &tmdbCfg,
		Notify:       &notifyCfg,
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

	// 2. 导入采集站或计划任务前，阻断新采集并等待在途写入和发布彻底排空
	if req.Modules.FilmSources || req.Modules.CronTasks {
		if err := spider.StopAllTasksAndWait(0); err != nil {
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

	if req.Modules.CronTasks {
		tasks := backup.CronTasks
		// 记录旧任务用于后续卸载运行时
		oldTasks, err := repository.GetAllFilmTask()
		if err != nil {
			return err
		}
		// 必须先写库成功，再替换运行时，避免写库失败时运行时已被卸掉
		if err := repository.ResetFilmTasks(tasks); err != nil {
			return fmt.Errorf("导入定时任务失败: %w", err)
		}
		for _, t := range oldTasks {
			spider.RemoveCronByTaskId(t.Id)
		}
		reloaded, err := repository.GetAllFilmTask()
		if err != nil {
			return err
		}
		var reloadErr error
		for _, t := range reloaded {
			if err := spider.ReloadCronTask(t.Id); err != nil {
				syslog.Errorf("[Backup] 重载定时任务 %s 失败: %v", t.Id, err)
				if reloadErr == nil {
					reloadErr = fmt.Errorf("重载定时任务 %s 失败: %w", t.Id, err)
				}
			}
		}
		if reloadErr != nil {
			return reloadErr
		}
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
	if req.Modules.CronTasks && b.CronTasks == nil {
		return errors.New("备份中缺少定时任务")
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
	return nil
}
