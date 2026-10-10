package service

import (
	"errors"
	"log"
	"strings"
	"sync/atomic"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider"
)

var (
	bannerPrimaryEpoch      atomic.Uint64
	errBannerPrimaryChanged = errors.New("首选站已切换，本次排片作废")
)

func bannerPrimaryEpochNow() uint64 {
	return bannerPrimaryEpoch.Load()
}

func bumpBannerPrimaryEpoch() uint64 {
	return bannerPrimaryEpoch.Add(1)
}

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

// UpdateConfig 更新轮播配置并联动计划任务。
// 刮削总开关未就绪时，界面上的「影片是否刮削」是关，保存也写成关。
func (s *BannerAutoService) UpdateConfig(cfg model.BannerConfig) error {
	if !tmdbScrapeReady(repository.GetTMDBConfig()) {
		cfg.AutoTMDB = false
	}
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

// RefreshAfterTMDBEnabled 刮削总开关从关到开时，轮播刮削本来就是开的自动模式立刻换一批。
func (s *BannerAutoService) RefreshAfterTMDBEnabled() {
	cfg := repository.GetBannerConfig()
	if cfg.Mode != repository.BannerModeAuto || !cfg.AutoTMDB {
		return
	}
	if _, err := s.StartBannerGenerateTask("tmdb_enabled"); err != nil {
		log.Printf("[BannerAuto] 刮削开启后换一批失败: %v", err)
		return
	}
	log.Printf("[BannerAuto] 刮削已开启且轮播刮削为开，开始换一批")
}

// RefreshAfterPrimarySwitch 自动排片开启时，清空当前轮播并按新首选站重新获取一次。
func (s *BannerAutoService) RefreshAfterPrimarySwitch() {
	if repository.GetBannerConfig().Mode != repository.BannerModeAuto {
		return
	}
	bumpBannerPrimaryEpoch()
	if err := repository.SaveBanners(model.Banners{}); err != nil {
		log.Printf("[BannerAuto] 切换首选站清空轮播失败: %v", err)
	}
	if _, err := s.StartBannerGenerateTask("primary_source_switch"); err != nil {
		log.Printf("[BannerAuto] 切换首选站后启动排片失败: %v", err)
	}
	log.Printf("[BannerAuto] 首选站已切换，已清空轮播并按新首选站重新排片")
}

func abortIfBannerEpochMoved(epoch uint64) error {
	if bannerPrimaryEpochNow() != epoch {
		return errBannerPrimaryChanged
	}
	return nil
}

func (s *BannerAutoService) finishAndMaybeRetry(err error) {
	FinishBannerGenerateProgress(err)
	if errors.Is(err, errBannerPrimaryChanged) {
		log.Printf("[BannerAuto] 排片期间首选站已切换，重新获取一次")
		_, _ = s.StartBannerGenerateTask("primary_source_switch")
	}
}
