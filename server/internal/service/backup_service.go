package service

import (
	"errors"
	"fmt"
	"time"

	"server/internal/config"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
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

	// 导入采集站前先停采集，避免写入冲突
	if req.Modules.FilmSources || req.Modules.CronTasks {
		spider.StopAllTasks()
	}

	if req.Modules.Site {
		if backup.Site == nil {
			return errors.New("备份中缺少网站配置数据")
		}
		if err := repository.SaveSiteBasic(*backup.Site); err != nil {
			return fmt.Errorf("导入网站配置失败: %w", err)
		}
	}

	if req.Modules.FilmSources {
		list := backup.FilmSources
		if list == nil {
			list = []model.FilmSource{}
		}
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
	}

	if req.Modules.CronTasks {
		tasks := backup.CronTasks
		if tasks == nil {
			tasks = []model.FilmCollectTask{}
		}
		// 先卸掉旧任务运行时
		oldTasks, err := repository.GetAllFilmTask()
		if err != nil {
			return err
		}
		for _, t := range oldTasks {
			spider.RemoveCronByTaskId(t.Id)
		}
		if err := repository.ResetFilmTasks(tasks); err != nil {
			return fmt.Errorf("导入定时任务失败: %w", err)
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
		if banners == nil {
			banners = model.Banners{}
		}
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
		if exports == nil {
			exports = []model.MappingRuleExport{}
		}
		if err := repository.ReplaceMappingRules(exports); err != nil {
			return fmt.Errorf("导入映射规则失败: %w", err)
		}
	}

	if req.Modules.Proxy {
		if backup.Proxy == nil {
			return errors.New("备份中缺少网络代理配置数据")
		}
		if err := repository.SaveProxyConfig(*backup.Proxy); err != nil {
			return fmt.Errorf("导入网络代理配置失败: %w", err)
		}
		NotifySvc.EnsureBotPoller()
	}

	if req.Modules.TMDB {
		if backup.TMDB == nil {
			return errors.New("备份中缺少 TMDB 刮削配置数据")
		}
		if err := repository.SaveTMDBConfig(*backup.TMDB); err != nil {
			return fmt.Errorf("导入 TMDB 刮削配置失败: %w", err)
		}
	}

	if req.Modules.Notify {
		if backup.Notify == nil {
			return errors.New("备份中缺少通知配置数据")
		}
		if err := repository.SaveNotifyConfig(*backup.Notify); err != nil {
			return fmt.Errorf("导入通知配置失败: %w", err)
		}
		NotifySvc.EnsureBotPoller()
	}

	return nil
}
