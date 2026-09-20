package service

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/repository/support"
	"server/internal/spider"
	"server/internal/utils"
)

type BannerAutoService struct{}

var BannerAutoSvc = new(BannerAutoService)

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

// GenerateAutoBanners 依据当前配置策略自动生成首页轮播
func (s *BannerAutoService) GenerateAutoBanners(ctx context.Context, triggerSource string) (model.Banners, error) {
	return s.generateAutoBannersWithConfig(ctx, repository.GetBannerConfig(), triggerSource)
}

// generateAutoBannersWithConfig 开启刮削时缺横图走顶替；关闭刮削时用竖版海报落库兜底。
func (s *BannerAutoService) generateAutoBannersWithConfig(ctx context.Context, cfg model.BannerConfig, triggerSource string) (model.Banners, error) {
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

	// 3. 抽取最终轮播影片（全新候选影片优先全量随机抽取，彻底杜绝局限在少数旧片或旧刮削片中）
	pickedSnaps := make([]model.FilmListSnapshot, 0, needCount)
	reusedCount := 0

	// 3.1 优先从全新候选池中全量随机抽取目标数量
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

	// 3.2 仅在全新影片穷尽仍不足目标数量时，才从当前在展影片中补足差额
	if len(pickedSnaps) < needCount && len(existingCandidates) > 0 {
		stillNeed := needCount - len(pickedSnaps)
		reusedIndices := r.Perm(len(existingCandidates))
		for i := 0; i < stillNeed && i < len(existingCandidates); i++ {
			pickedSnaps = append(pickedSnaps, existingCandidates[reusedIndices[i]])
			reusedCount++
		}
	}

	// 3.3 兜底约束：候选库中必须至少有可用影片
	if len(pickedSnaps) == 0 && len(pinnedBanners) == 0 {
		return nil, fmt.Errorf("候选库中无有效影片数据，请先采集影视数据")
	}

	// 4. 按需 TMDB 自动刮削：仅在开启 TMDB 且配置有效 Key 时，对已选出但缺少横图的影片发起并发刮削
	scrapedAttemptCount := 0
	scrapedSuccessCount := 0
	localSlideCount := 0

	for _, snap := range pickedSnaps {
		if strings.TrimSpace(snap.DisplayPictureSlide()) != "" {
			localSlideCount++
		}
	}

	if canAutoTMDB {
		toScrapeIndices := make([]int, 0, len(pickedSnaps))
		for i, snap := range pickedSnaps {
			if strings.TrimSpace(snap.DisplayPictureSlide()) == "" {
				toScrapeIndices = append(toScrapeIndices, i)
			}
		}

		if len(toScrapeIndices) > 0 {
			scrapedAttemptCount = len(toScrapeIndices)
			log.Printf("[BannerAuto] 策略已选出 %d 部轮播影片 (本地已有横图 %d 部)，对缺少横图的 %d 部影片按需发起 TMDB 刮削",
				len(pickedSnaps), localSlideCount, scrapedAttemptCount)

			type scrapeResult struct {
				idx  int
				snap *model.FilmListSnapshot
				err  error
			}
			jobs := make(chan int, scrapedAttemptCount)
			for _, idx := range toScrapeIndices {
				jobs <- idx
			}
			close(jobs)
			concurrency := 3
			if scrapedAttemptCount < concurrency {
				concurrency = scrapedAttemptCount
			}
			results := make(chan scrapeResult, scrapedAttemptCount)
			var wg sync.WaitGroup

			for w := 0; w < concurrency; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for idx := range jobs {
						select {
						case <-ctx.Done():
							return
						default:
						}
						updated, ok, err := s.tryTMDBAutoScrape(pickedSnaps[idx], version)
						if err != nil {
							results <- scrapeResult{idx: idx, snap: nil, err: err}
						} else if ok && updated != nil && strings.TrimSpace(updated.DisplayPictureSlide()) != "" {
							results <- scrapeResult{idx: idx, snap: updated, err: nil}
						} else {
							results <- scrapeResult{idx: idx, snap: nil, err: nil}
						}
					}
				}()
			}

			go func() {
				wg.Wait()
				close(results)
			}()

			var lastTMDBErr error
			for r := range results {
				if r.err != nil {
					lastTMDBErr = r.err
				} else if r.snap != nil {
					scrapedSuccessCount++
					pickedSnaps[r.idx] = *r.snap
				}
			}

			if lastTMDBErr != nil {
				log.Printf("[BannerAuto] TMDB 刮削过程中记录到错误 (已优雅降级): %v", lastTMDBErr)
			}
			log.Printf("[BannerAuto] TMDB 刮削完成: 计划刮削 %d 个，成功获取横图 %d 个",
				scrapedAttemptCount, scrapedSuccessCount)
		}

		// 4.1 缺横图顶替：优先全新候选已有横图，穷尽后再回退当前在展旧片
		var extraReused int
		pickedSnaps, extraReused = replaceMissingSlides(pickedSnaps, freshCandidates, existingCandidates, pinnedMap, r)
		reusedCount += extraReused
	}

	// 5. 组合最终轮播
	finalBanners := make(model.Banners, 0, len(pinnedBanners)+len(pickedSnaps))
	finalBanners = append(finalBanners, pinnedBanners...)
	for _, snap := range pickedSnaps {
		finalBanners = append(finalBanners, bannerFromSnapshot(snap, len(finalBanners)+1, fallbackPosterSlide))
	}

	if len(finalBanners) == 0 {
		return nil, fmt.Errorf("未能生成有效轮播项")
	}

	// 6. 持久化并刷新前台缓存
	if err := repository.SaveBanners(finalBanners); err != nil {
		return nil, fmt.Errorf("保存生成轮播失败: %w", err)
	}
	support.ClearIndexPageCache()

	scrapeInfo := "刮削: 0 个"
	if scrapedAttemptCount > 0 {
		scrapeInfo = fmt.Sprintf("刮削: %d 个 [成功: %d 个]", scrapedAttemptCount, scrapedSuccessCount)
	}
	log.Printf("[BannerAuto] 自动排片完成: 从 %d 个候选影片里面获取 %d 个轮播 (全新: %d 个, 本地已有横图: %d 个, %s, 复用旧轮播: %d 个, 置顶项: %d 个, 策略: %s, 触发源: %s)",
		len(candidates), len(finalBanners), len(pickedSnaps)-reusedCount, localSlideCount, scrapeInfo, reusedCount, len(pinnedBanners), cfg.Strategy, triggerSource)
	return finalBanners, nil
}

// tryTMDBAutoScrape 尝试通过 TMDB 检索并自动应用横图元数据
func (s *BannerAutoService) tryTMDBAutoScrape(snap model.FilmListSnapshot, version string) (*model.FilmListSnapshot, bool, error) {
	yearStr := ""
	if snap.Year > 0 {
		yearStr = strconv.FormatInt(snap.Year, 10)
	}
	candidates, err := TMDBSvc.Search(snap.Name, yearStr, "")
	if err != nil {
		return nil, false, err
	}
	if len(candidates) == 0 {
		return nil, false, nil
	}

	var matched *model.TMDBCandidate
	cleanedTarget, _ := CleanKeywordForSearch(snap.Name)

	for i := range candidates {
		c := &candidates[i]
		if strings.TrimSpace(c.Backdrop) == "" {
			continue
		}

		cleanedCand, _ := CleanKeywordForSearch(c.Title)
		if cleanedCand == "" {
			cleanedCand = strings.TrimSpace(c.Title)
		}

		// 名称精准或包含匹配
		nameMatches := cleanedCand == cleanedTarget ||
			strings.EqualFold(c.OriginalTitle, snap.Name) ||
			strings.Contains(cleanedTarget, cleanedCand) ||
			strings.Contains(cleanedCand, cleanedTarget)

		if !nameMatches {
			continue
		}

		// 年份容差 (同一年份或未知)
		if snap.Year > 0 && c.Year != "" {
			candY, _ := strconv.Atoi(c.Year)
			if candY > 0 && (int(snap.Year)-candY > 1 || candY-int(snap.Year) > 1) {
				continue
			}
		}

		matched = c
		break
	}

	if matched == nil {
		return nil, false, nil
	}

	// 排片刮削只补 16:9 横图，不改海报/简介/评分，避免把片库打成自定义封面锁定
	applyReq := model.TMDBApplyReq{
		Mid:       snap.Mid,
		TmdbID:    matched.ID,
		MediaType: matched.MediaType,
		Fields:    []string{"backdrop"},
	}

	if err := TMDBSvc.ApplyDetail(applyReq); err != nil {
		log.Printf("[BannerAuto] TMDB 自动应用详情失败 mid=%d tmdbId=%d: %v", snap.Mid, matched.ID, err)
		return nil, false, err
	}

	// 重新读取写回后的快照
	updated := filmsnapshot.GetSnapshotByMid(version, snap.Mid)
	if updated != nil && strings.TrimSpace(updated.DisplayPictureSlide()) != "" {
		return updated, true, nil
	}
	return nil, false, nil
}

func normalizeBannerTargetCount(count int) int {
	if count <= 0 {
		return repository.DefaultBannerCount
	}
	if count > repository.MaxBannerCount {
		return repository.MaxBannerCount
	}
	return count
}

// replaceMissingSlides 优先用全新候选中已有横图顶替；全新池耗尽才回退当前在展旧片。
func replaceMissingSlides(
	picked []model.FilmListSnapshot,
	freshCandidates []model.FilmListSnapshot,
	existingCandidates []model.FilmListSnapshot,
	pinnedMap map[int64]struct{},
	r *rand.Rand,
) ([]model.FilmListSnapshot, int) {
	if r == nil {
		r = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	existingPickedMap := make(map[int64]struct{}, len(picked)+len(pinnedMap))
	for k := range pinnedMap {
		existingPickedMap[k] = struct{}{}
	}
	for _, s := range picked {
		if s.Mid > 0 {
			existingPickedMap[s.Mid] = struct{}{}
		}
	}

	existingMidSet := make(map[int64]struct{}, len(existingCandidates))
	for _, e := range existingCandidates {
		if e.Mid > 0 {
			existingMidSet[e.Mid] = struct{}{}
		}
	}

	pickWithSlide := func(pool []model.FilmListSnapshot) []model.FilmListSnapshot {
		out := make([]model.FilmListSnapshot, 0)
		for _, c := range pool {
			if c.Mid <= 0 {
				continue
			}
			if _, exists := existingPickedMap[c.Mid]; exists {
				continue
			}
			if strings.TrimSpace(c.DisplayPictureSlide()) != "" {
				out = append(out, c)
			}
		}
		r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
		return out
	}

	freshRepl := pickWithSlide(freshCandidates)
	oldRepl := pickWithSlide(existingCandidates)

	extraReused := 0
	fi, oi := 0, 0
	for i, snap := range picked {
		if strings.TrimSpace(snap.DisplayPictureSlide()) != "" {
			continue
		}
		var sub model.FilmListSnapshot
		switch {
		case fi < len(freshRepl):
			sub = freshRepl[fi]
			fi++
		case oi < len(oldRepl):
			sub = oldRepl[oi]
			oi++
			if _, wasExisting := existingMidSet[snap.Mid]; !wasExisting {
				extraReused++
			}
		default:
			continue
		}
		existingPickedMap[sub.Mid] = struct{}{}
		log.Printf("[BannerAuto] 影片 [%s](mid=%d) 未能刮削到横屏大图，已自动挑选已有横图影片 [%s](mid=%d) 顶替",
			snap.Name, snap.Mid, sub.Name, sub.Mid)
		picked[i] = sub
	}
	return picked, extraReused
}

func bannerFromSnapshot(snap model.FilmListSnapshot, sortOrder int, fallbackPosterSlide bool) model.Banner {
	pic := strings.TrimSpace(snap.DisplayPicture())
	slide := strings.TrimSpace(snap.DisplayPictureSlide())
	if fallbackPosterSlide && slide == "" && pic != "" {
		slide = pic
	}

	return model.Banner{
		Id:            utils.GenerateSalt(),
		Mid:           snap.Mid,
		Name:          snap.Name,
		Year:          snap.Year,
		CName:         snap.CName,
		Poster:        pic,
		Picture:       pic,
		PictureSlide:  slide,
		CustomPicture: pic,
		Remark:        snap.Remarks,
		Sort:          int64(sortOrder),
		IsCustomPic:   true,
	}
}
