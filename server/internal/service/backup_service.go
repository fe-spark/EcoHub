package service

import (
	"errors"
	"fmt"
	"time"

	"server/internal/config"
	"server/internal/model"
	"server/internal/notify"
	"server/internal/repository"
	"server/internal/spider"
)

// BackupService 站点配置备份/恢复（不含影视库存与账号）
type BackupService struct{}

var BackupSvc = new(BackupService)

// ExportConfig 导出当前可迁移配置
func (s *BackupService) ExportConfig() model.ConfigBackup {
	sources := repository.GetCollectSourceList()
	if sources == nil {
		sources = []model.FilmSource{}
	}
	tasks := repository.GetAllFilmTask()
	if tasks == nil {
		tasks = []model.FilmCollectTask{}
	}
	banners := repository.GetBanners()
	if banners == nil {
		banners = model.Banners{}
	}

	site := repository.GetSiteBasic()
	notifyCfg := repository.GetNotifyConfig()

	rules, _ := repository.ListAllMappingRules(repository.MappingRuleQuery{})
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
		Notify:       &notifyCfg,
		MappingRules: exports,
	}
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
		if len(list) > MaxCollectSources {
			return fmt.Errorf("备份中采集站数量超过上限（%d）", MaxCollectSources)
		}
		// 导入前清理旧站限流器
		for _, src := range repository.GetCollectSourceList() {
			spider.ClearLimiter(src.Id)
		}
		if err := repository.ReplaceCollectSources(list); err != nil {
			return fmt.Errorf("导入采集站失败: %w", err)
		}
		for _, src := range list {
			spider.ClearLimiter(src.Id)
		}
		clearProvideNetworkConfigCache()
	}

	if req.Modules.CronTasks {
		tasks := backup.CronTasks
		if tasks == nil {
			tasks = []model.FilmCollectTask{}
		}
		// 先卸掉旧任务运行时
		for _, t := range repository.GetAllFilmTask() {
			spider.RemoveCronByTaskId(t.Id)
		}
		if err := repository.ResetFilmTasks(tasks); err != nil {
			return fmt.Errorf("导入定时任务失败: %w", err)
		}
		for _, t := range repository.GetAllFilmTask() {
			if err := spider.ReloadCronTask(t.Id); err != nil {
				// 单任务重载失败不阻断整批导入
				_ = err
			}
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
	}

	if req.Modules.Notify {
		if backup.Notify == nil {
			return errors.New("备份中缺少通知配置数据")
		}
		if err := repository.SaveNotifyConfig(*backup.Notify); err != nil {
			return fmt.Errorf("导入通知配置失败: %w", err)
		}
		notify.EnsureBotPoller()
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

	return nil
}
