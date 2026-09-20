package service

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/repository/support"
)

type BannerAutoService struct{}

var BannerAutoSvc = new(BannerAutoService)

// GenerateAutoBanners 依据当前配置策略自动生成首页轮播
func (s *BannerAutoService) GenerateAutoBanners(ctx context.Context, triggerSource string) (model.Banners, error) {
	return s.generateAutoBannersWithConfig(ctx, repository.GetBannerConfig(), triggerSource)
}

// generateAutoBannersWithConfig 开启刮削时缺额持续并发刮削至满额（最长不超过5分钟），关闭刮削时走常规抽取。
func (s *BannerAutoService) generateAutoBannersWithConfig(ctx context.Context, cfg model.BannerConfig, triggerSource string) (model.Banners, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	scrapeCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	version := filmsnapshot.GetActiveSnapshotVersion()
	if version == "" {
		return nil, fmt.Errorf("当前系统无可用影片快照版本，请先采集影视数据")
	}

	targetCount := normalizeBannerTargetCount(cfg.Count)
	tmdbCfg := repository.GetTMDBConfig()
	canAutoTMDB := cfg.AutoTMDB && tmdbCfg.Enabled && strings.TrimSpace(tmdbCfg.ApiKey) != ""
	fallbackPosterSlide := !canAutoTMDB

	pinnedMap := make(map[int64]struct{}, len(cfg.PinnedMids))
	var pinnedBanners model.Banners

	// 1. 加载管理员置顶影片
	if len(cfg.PinnedMids) > 0 {
		snaps := filmsnapshot.GetSnapshotsByMidsOrdered(version, cfg.PinnedMids)
		for _, snap := range snaps {
			if snap.Mid <= 0 {
				continue
			}
			pinnedMap[snap.Mid] = struct{}{}
			pinnedBanners = append(pinnedBanners, bannerFromSnapshot(snap, len(pinnedBanners)+1, fallbackPosterSlide))
		}
	}

	needCount := targetCount - len(pinnedBanners)
	if needCount <= 0 {
		if err := repository.SaveBanners(pinnedBanners); err != nil {
			return nil, err
		}
		support.ClearIndexPageCache()
		return pinnedBanners, nil
	}

	// 2. 提取候选影片大池 (扩大至 200 部，确保片源丰富多样)
	candidates := filmsnapshot.GetSnapshotBannerCandidates(version, cfg.Strategy, cfg.Categories, 200)
	if len(candidates) == 0 {
		if len(cfg.Categories) > 0 {
			candidates = filmsnapshot.GetSnapshotBannerCandidates(version, cfg.Strategy, nil, 200)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("片库中无候选影片，请先采集影视数据")
	}

	// 2.1 收集当前已有轮播的影片 Mid，用于轮换去重（避免换来换去老是同一批）
	currentBanners := repository.GetBanners()
	currentMids := make(map[int64]struct{}, len(currentBanners))
	for _, b := range currentBanners {
		if b.Mid > 0 {
			currentMids[b.Mid] = struct{}{}
		}
	}

	// 2.2 全局随机打散候选池 (Fisher-Yates Shuffle)，打破固定前排顺序
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})

	// 2.3 轮换去重分流：分离出全新候选影片与当前在展影片
	freshCandidates := make([]model.FilmListSnapshot, 0, len(candidates))
	existingCandidates := make([]model.FilmListSnapshot, 0, len(currentMids))
	for _, snap := range candidates {
		if snap.Mid <= 0 {
			continue
		}
		if _, isPinned := pinnedMap[snap.Mid]; isPinned {
			continue
		}
		if _, exists := currentMids[snap.Mid]; exists {
			existingCandidates = append(existingCandidates, snap)
		} else {
			freshCandidates = append(freshCandidates, snap)
		}
	}
	log.Printf("[BannerAuto] 开始排片(策略=%s, 目标=%d部, 触发源=%s): 从片库筛选出 %d 个候选影片 (全新候选: %d 个, 轮换排除当前已展: %d 个)",
		cfg.Strategy, targetCount, triggerSource, len(candidates), len(freshCandidates), len(existingCandidates))

	var (
		pickedSnaps         []model.FilmListSnapshot
		reusedCount         int
		scrapedAttemptCount int
		scrapedSuccessCount int
	)

	if canAutoTMDB {
		// 3.1 开启 TMDB 自动刮削：从候选池持续并发刮削，直到有效影片达到目标数量或超时 5 分钟
		pickedSnaps, scrapedAttemptCount, scrapedSuccessCount, reusedCount = s.scrapeUntilTargetCount(
			scrapeCtx, needCount, freshCandidates, existingCandidates, version,
		)
	} else {
		// 3.2 未开启 TMDB 刮削：优先全新候选全量随机抽取，差额回退在展影片
		pickedSnaps = make([]model.FilmListSnapshot, 0, needCount)
		if len(freshCandidates) > 0 {
			pickFromFresh := needCount
			if len(freshCandidates) < pickFromFresh {
				pickFromFresh = len(freshCandidates)
			}
			indices := r.Perm(len(freshCandidates))
			for i := 0; i < pickFromFresh; i++ {
				pickedSnaps = append(pickedSnaps, freshCandidates[indices[i]])
			}
		}

		if len(pickedSnaps) < needCount && len(existingCandidates) > 0 {
			stillNeed := needCount - len(pickedSnaps)
			reusedIndices := r.Perm(len(existingCandidates))
			for i := 0; i < stillNeed && i < len(existingCandidates); i++ {
				pickedSnaps = append(pickedSnaps, existingCandidates[reusedIndices[i]])
				reusedCount++
			}
		}
	}

	// 3.3 兜底约束：候选库中必须至少有可用影片
	if len(pickedSnaps) == 0 && len(pinnedBanners) == 0 {
		return nil, fmt.Errorf("候选库中无有效影片数据，请先采集影视数据")
	}

	localSlideCount := 0
	for _, snap := range pickedSnaps {
		if strings.TrimSpace(snap.DisplayPictureSlide()) != "" {
			localSlideCount++
		}
	}

	// 4. 组合最终轮播条目
	finalBanners := make(model.Banners, 0, len(pinnedBanners)+len(pickedSnaps))
	finalBanners = append(finalBanners, pinnedBanners...)
	for _, snap := range pickedSnaps {
		finalBanners = append(finalBanners, bannerFromSnapshot(snap, len(finalBanners)+1, fallbackPosterSlide))
	}

	if len(finalBanners) == 0 {
		return nil, fmt.Errorf("未能生成有效轮播项")
	}

	// 5. 持久化并刷新前台缓存
	if err := repository.SaveBanners(finalBanners); err != nil {
		return nil, fmt.Errorf("保存生成轮播失败: %w", err)
	}
	support.ClearIndexPageCache()

	scrapeInfo := "刮削: 0 个"
	if scrapedAttemptCount > 0 {
		scrapeInfo = fmt.Sprintf("刮削: %d 个 [有效成功: %d 个]", scrapedAttemptCount, scrapedSuccessCount)
	}
	log.Printf("[BannerAuto] 自动排片完成: 从 %d 个候选影片里面获取 %d 个轮播 (全新: %d 个, 本地已有横图: %d 个, %s, 复用旧轮播: %d 个, 置顶项: %d 个, 策略: %s, 触发源: %s)",
		len(candidates), len(finalBanners), len(pickedSnaps)-reusedCount, localSlideCount, scrapeInfo, reusedCount, len(pinnedBanners), cfg.Strategy, triggerSource)
	return finalBanners, nil
}
