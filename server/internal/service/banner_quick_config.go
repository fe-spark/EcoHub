package service

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"server/internal/model"
	"server/internal/repository"
)

// BannerQuickConfigInput 一键排片弹窗保存的配置。定时在计划任务里配置。
type BannerQuickConfigInput struct {
	Strategy   string              `json:"strategy"`
	AutoScrape bool                `json:"autoScrape"`
	Quotas     []model.BannerQuota `json:"quotas"`
}

// BannerQuickConfigView 弹窗读取的排片配置。
type BannerQuickConfigView struct {
	Saved      bool                `json:"saved"`
	Strategy   string              `json:"strategy"`
	AutoScrape bool                `json:"autoScrape"`
	Quotas     []model.BannerQuota `json:"quotas"`
}

var bannerScheduleRunning atomic.Bool

func (s *ManageService) GetBannerQuickConfig() (BannerQuickConfigView, error) {
	cfg, exists, err := repository.LoadBannerConfig()
	if err != nil {
		return BannerQuickConfigView{}, err
	}
	if !exists {
		defaultCfg, ok, err := s.EnsureBannerQuickDefaultConfig()
		if err != nil {
			return BannerQuickConfigView{}, err
		}
		if ok && len(defaultCfg.Quotas) > 0 {
			return BannerQuickConfigView{
				Saved:      true,
				Strategy:   quickStrategyName(defaultCfg.Strategy),
				AutoScrape: defaultCfg.AutoTMDB,
				Quotas:     defaultCfg.Quotas,
			}, nil
		}
		return BannerQuickConfigView{
			Saved:    false,
			Strategy: "hot",
			Quotas:   []model.BannerQuota{},
		}, nil
	}
	if cfg.Quotas == nil {
		cfg.Quotas = []model.BannerQuota{}
	}
	return BannerQuickConfigView{
		Saved:      true,
		Strategy:   quickStrategyName(cfg.Strategy),
		AutoScrape: cfg.AutoTMDB,
		Quotas:     cfg.Quotas,
	}, nil
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
	return s.GetBannerQuickConfig()
}

// ResetBannerDeckForSourceChange 切换首选站后清空轮播和排片规则。
func ResetBannerDeckForSourceChange() error {
	var first error
	if err := ManageSvc.SaveBanners(model.Banners{}); err != nil {
		first = err
	}
	if err := repository.DeleteBannerConfig(); err != nil && first == nil {
		first = err
	}
	return first
}

// EnsureBannerQuickDefaultConfig 确保当前主站有可用的排片规则。
// 若尚未保存过配置或切换主站后为空，按当前主站一级可见分类默认每类 1 部自动创建默认排片规则。
func (s *ManageService) EnsureBannerQuickDefaultConfig() (model.BannerConfig, bool, error) {
	cfg, exists, err := repository.LoadBannerConfig()
	if err != nil {
		return model.BannerConfig{}, false, err
	}
	if exists && len(cfg.Quotas) > 0 {
		return cfg, true, nil
	}
	cats, err := IndexSvc.GetNavCategory()
	if err != nil {
		return model.BannerConfig{}, false, err
	}
	if len(cats) == 0 {
		return model.BannerConfig{}, false, nil
	}
	quotas := make([]model.BannerQuota, 0, len(cats))
	for _, c := range cats {
		if c.Id > 0 && c.Show {
			quotas = append(quotas, model.BannerQuota{
				CategoryID: c.Id,
				Count:      1,
			})
		}
	}
	if len(quotas) == 0 {
		return model.BannerConfig{}, false, nil
	}
	defaultCfg := repository.NormalizeBannerConfig(model.BannerConfig{
		Strategy: repository.BannerStrategyHot,
		Quotas:   quotas,
	})
	saved, _, err := repository.SaveBannerConfigIfEmpty(defaultCfg)
	if err != nil {
		return model.BannerConfig{}, false, err
	}
	return saved, len(saved.Quotas) > 0, nil
}

// bannerQuickHasQuotas 检查排片配置里是否有正数量。若无配置且主站已存在分类，自动补建默认排片规则。
func bannerQuickHasQuotas() (bool, error) {
	cfg, exists, err := repository.LoadBannerConfig()
	if err != nil {
		return false, err
	}
	if exists {
		return len(cfg.Quotas) > 0, nil
	}
	_, ok, err := ManageSvc.EnsureBannerQuickDefaultConfig()
	return ok, err
}

// RunBannerQuickSchedule 按已存排片规则生成并替换线上轮播。
func (s *ManageService) RunBannerQuickSchedule() error {
	cfg, err := repository.GetBannerConfig()
	if err != nil {
		return err
	}
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
		defaultCfg, ok, err := s.EnsureBannerQuickDefaultConfig()
		if err != nil {
			return err
		}
		if ok {
			cfg = defaultCfg
			for _, quota := range cfg.Quotas {
				if quota.CategoryID > 0 && quota.Count > 0 {
					quotas = append(quotas, CategoryQuota{CategoryID: quota.CategoryID, Count: quota.Count})
				}
			}
		}
	}
	if len(quotas) == 0 {
		return fmt.Errorf("未配置排片数量，且当前主站无可用分类")
	}

	var generated model.Banners
	sawDone := false
	err = s.OneClickGenerateDeckStream(context.Background(), OneClickGenerateReq{
		Strategy: cfg.Strategy,
		Quotas:   quotas,
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
