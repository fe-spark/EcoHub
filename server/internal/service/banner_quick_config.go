package service

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider"
)

func init() {
	spider.SetBannerQuickRunner(func() error {
		return ManageSvc.RunBannerQuickSchedule()
	})
}

// BannerQuickConfigInput 一键排片弹窗保存的配方。定时在计划任务里配置。
type BannerQuickConfigInput struct {
	Strategy   string              `json:"strategy"`
	AutoScrape bool                `json:"autoScrape"`
	Quotas     []model.BannerQuota `json:"quotas"`
}

// BannerQuickConfigView 弹窗读取的配方。Saved 为 false 时前端使用每类 1 部。
type BannerQuickConfigView struct {
	Saved      bool                `json:"saved"`
	Strategy   string              `json:"strategy"`
	AutoScrape bool                `json:"autoScrape"`
	Quotas     []model.BannerQuota `json:"quotas"`
}

var bannerScheduleRunning atomic.Bool

func (s *ManageService) GetBannerQuickConfig() BannerQuickConfigView {
	if !repository.BannerConfigExists() {
		return BannerQuickConfigView{
			Strategy: "hot",
			Quotas:   []model.BannerQuota{},
		}
	}
	cfg := repository.GetBannerConfig()
	if cfg.Quotas == nil {
		cfg.Quotas = []model.BannerQuota{}
	}
	return BannerQuickConfigView{
		Saved:      true,
		Strategy:   quickStrategyName(cfg.Strategy),
		AutoScrape: cfg.AutoTMDB,
		Quotas:     cfg.Quotas,
	}
}

func (s *ManageService) SaveBannerQuickConfig(in BannerQuickConfigInput) (BannerQuickConfigView, error) {
	strategy, err := storedQuickStrategy(in.Strategy)
	if err != nil {
		return BannerQuickConfigView{}, err
	}
	cfg := repository.NormalizeBannerConfig(model.BannerConfig{
		Strategy: strategy,
		AutoTMDB: in.AutoScrape,
		Quotas:   in.Quotas,
	})
	if err := repository.SaveBannerConfig(cfg); err != nil {
		return BannerQuickConfigView{}, fmt.Errorf("保存排片配置失败")
	}
	return s.GetBannerQuickConfig(), nil
}

// ResetBannerDeckForSourceChange 切换首选站后清空轮播和排片配方，并关闭定时排片。
func ResetBannerDeckForSourceChange() error {
	var first error
	if err := ManageSvc.SaveBanners(model.Banners{}); err != nil {
		first = err
	}
	if err := repository.DeleteBannerConfig(); err != nil && first == nil {
		first = err
	}
	if err := DisableBannerQuickSchedule(); err != nil && first == nil {
		first = err
	}
	return first
}

// bannerQuickHasQuotas 已保存配方里是否有正数量。
func bannerQuickHasQuotas() bool {
	if !repository.BannerConfigExists() {
		return false
	}
	return len(repository.GetBannerConfig().Quotas) > 0
}

// DisableBannerQuickSchedule 关闭计划任务里的定时排片。配方保留。
func DisableBannerQuickSchedule() error {
	task, err := repository.GetFilmTaskById(model.TaskIDBannerAuto)
	if err != nil {
		spider.RemoveCronByTaskId(model.TaskIDBannerAuto)
		return nil
	}
	if task.State {
		task.State = false
		if err := repository.UpdateFilmTask(task); err != nil {
			return err
		}
	}
	return spider.ReloadCronTask(task.Id)
}

// RunBannerQuickSchedule 按已存配方生成并替换线上轮播。
func (s *ManageService) RunBannerQuickSchedule() error {
	cfg := repository.GetBannerConfig()
	if !bannerScheduleRunning.CompareAndSwap(false, true) {
		return fmt.Errorf("定时排片正在执行")
	}
	defer bannerScheduleRunning.Store(false)

	quotas := make([]CategoryQuota, 0, len(cfg.Quotas))
	for _, quota := range cfg.Quotas {
		if quota.CategoryID > 0 && quota.Count > 0 {
			quotas = append(quotas, CategoryQuota{CategoryID: quota.CategoryID, Count: quota.Count})
		}
	}
	if len(quotas) == 0 {
		return fmt.Errorf("未配置排片数量")
	}

	var generated model.Banners
	sawDone := false
	err := s.OneClickGenerateDeckStream(context.Background(), OneClickGenerateReq{
		Strategy:   cfg.Strategy,
		Quotas:     quotas,
		AutoScrape: cfg.AutoTMDB,
	}, func(evt OneClickProgressEvent) {
		if evt.Event == "done" {
			generated = evt.Banners
			sawDone = true
		}
	})
	if err != nil {
		return err
	}
	if !sawDone || len(generated) == 0 {
		return fmt.Errorf("未检索到可用影片")
	}
	return s.SaveBannerDeck(generated)
}

func storedQuickStrategy(name string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "hot", repository.BannerStrategyHot:
		return repository.BannerStrategyHot, nil
	case "score", repository.BannerStrategyScore:
		return repository.BannerStrategyScore, nil
	case "latest", repository.BannerStrategyLatest:
		return repository.BannerStrategyLatest, nil
	default:
		return "", fmt.Errorf("排片策略无效")
	}
}

func quickStrategyName(stored string) string {
	switch stored {
	case repository.BannerStrategyScore:
		return "score"
	case repository.BannerStrategyLatest:
		return "latest"
	default:
		return "hot"
	}
}
