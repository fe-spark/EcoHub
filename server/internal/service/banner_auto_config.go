package service

import (
	"log"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider"
)

// GetConfig 获取当前轮播配置 (附带系统全局 TMDB 就绪状态)
func (s *BannerAutoService) GetConfig() model.BannerConfig {
	cfg := repository.GetBannerConfig()
	tmdbCfg := repository.GetTMDBConfig()
	cfg.TMDBReady = tmdbCfg.Enabled && strings.TrimSpace(tmdbCfg.ApiKey) != ""
	return cfg
}

// SyncWithCronTask 联动计划任务系统：排片模式切换时自动启停 sys_cron_banner_auto 任务
func (s *BannerAutoService) SyncWithCronTask(mode string, refreshCron string) {
	if db.Mdb == nil {
		return
	}
	task, err := repository.GetFilmTaskById(model.TaskIDBannerAuto)
	if err != nil {
		return
	}
	shouldRun := mode == repository.BannerModeAuto
	changed := false
	if refreshCron = strings.TrimSpace(refreshCron); refreshCron != "" && task.Spec != refreshCron {
		task.Spec = refreshCron
		changed = true
	}
	if task.State != shouldRun {
		task.State = shouldRun
		changed = true
	}
	if changed {
		if err := repository.UpdateFilmTask(task); err == nil {
			_ = spider.ReloadCronTask(task.Id)
		}
	}
}

// InitCron 服务启动或配置变更时对齐轮播计划任务
func (s *BannerAutoService) InitCron() {
	cfg := repository.GetBannerConfig()
	s.SyncWithCronTask(cfg.Mode, cfg.RefreshCron)
}

// UpdateConfig 更新轮播配置并联动计划任务
func (s *BannerAutoService) UpdateConfig(cfg model.BannerConfig) error {
	if err := repository.SaveBannerConfig(cfg); err != nil {
		return err
	}
	s.SyncWithCronTask(cfg.Mode, cfg.RefreshCron)
	return nil
}

// HandleTMDBDisabled 当 TMDB 被中途关闭或 API Key 被清空时记录日志；轮播排片保留原有模式，后续仅使用片库已有图片
func (s *BannerAutoService) HandleTMDBDisabled() {
	log.Printf("[BannerAuto] 检测到系统关闭 TMDB 影视刮削，后续自动排片将仅使用片库已有图片，不发起在线刮削")
}
