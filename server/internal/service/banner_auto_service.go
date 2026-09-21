package service

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/repository/support"
)

type BannerAutoService struct{}

var BannerAutoSvc = new(BannerAutoService)

// BannerGenerateProgress 轮播生成任务实时进度（供前端轮询）
type BannerGenerateProgress struct {
	Running   bool   `json:"running"`   // 是否正在生成排片
	Percent   int    `json:"percent"`   // 0-100 真实完成百分比
	Stage     string `json:"stage"`     // 当前阶段描述
	Error     string `json:"error"`     // 失败原因（失败时非空）
	UpdatedAt int64  `json:"updatedAt"` // 最近更新时间戳(ms)
}

var bannerProgress = struct {
	mu        sync.RWMutex
	running   bool
	percent   int
	stage     string
	errMsg    string
	updatedAt int64
}{}

// StartBannerGenerateProgress 标记排片任务开始。若已有任务正在运行返回 false
func StartBannerGenerateProgress() bool {
	bannerProgress.mu.Lock()
	defer bannerProgress.mu.Unlock()
	if bannerProgress.running {
		return false
	}
	bannerProgress.running = true
	bannerProgress.percent = 5
	bannerProgress.stage = "正在初始化排片任务..."
	bannerProgress.errMsg = ""
	bannerProgress.updatedAt = time.Now().UnixMilli()
	return true
}

// ReportBannerGenerateProgress 更新排片生成实时进度与阶段描述
func ReportBannerGenerateProgress(percent int, stage string) {
	bannerProgress.mu.Lock()
	defer bannerProgress.mu.Unlock()
	if percent > 0 {
		bannerProgress.percent = percent
	}
	if stage != "" {
		bannerProgress.stage = stage
	}
	bannerProgress.updatedAt = time.Now().UnixMilli()
}

// FinishBannerGenerateProgress 结束排片任务：成功置 100%，失败记录错误信息
func FinishBannerGenerateProgress(err error) {
	bannerProgress.mu.Lock()
	defer bannerProgress.mu.Unlock()
	bannerProgress.running = false
	bannerProgress.updatedAt = time.Now().UnixMilli()
	if err != nil {
		bannerProgress.errMsg = err.Error()
		if bannerProgress.stage == "" {
			bannerProgress.stage = "排片生成失败"
		}
	} else {
		bannerProgress.percent = 100
		bannerProgress.stage = "排片完成，实时生效"
		bannerProgress.errMsg = ""
	}
}

// GetBannerGenerateProgress 获取当前轮播生成任务进度
func (s *BannerAutoService) GetBannerGenerateProgress() BannerGenerateProgress {
	bannerProgress.mu.RLock()
	defer bannerProgress.mu.RUnlock()
	stage := bannerProgress.stage
	if !bannerProgress.running && stage == "" {
		stage = "空闲"
	}
	return BannerGenerateProgress{
		Running:   bannerProgress.running,
		Percent:   bannerProgress.percent,
		Stage:     stage,
		Error:     bannerProgress.errMsg,
		UpdatedAt: bannerProgress.updatedAt,
	}
}

// StartBannerGenerateTask 异步启动自动生成轮播任务（供前台触发后轮询，彻底解决 504 Gateway Timeout）
func (s *BannerAutoService) StartBannerGenerateTask(triggerSource string) (BannerGenerateProgress, error) {
	if !StartBannerGenerateProgress() {
		return s.GetBannerGenerateProgress(), nil
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[BannerAuto] 异步排片任务发生异常: %v", r)
				FinishBannerGenerateProgress(fmt.Errorf("排片任务异常: %v", r))
			}
		}()

		// 异步执行，拥有充足的 5 分钟超时预算完成 TMDB 检索与封面刮削
		taskCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		_, err := s.generateAutoBannersWithConfig(taskCtx, repository.GetBannerConfig(), triggerSource)
		FinishBannerGenerateProgress(err)
	}()

	return s.GetBannerGenerateProgress(), nil
}

// GenerateAutoBanners 依据当前配置策略自动生成首页轮播（同步执行）
func (s *BannerAutoService) GenerateAutoBanners(ctx context.Context, triggerSource string) (model.Banners, error) {
	if !StartBannerGenerateProgress() {
		return nil, fmt.Errorf("排片任务正在执行中，已忽略本次重复调度 (触发源: %s)", triggerSource)
	}
	var (
		banners model.Banners
		err     error
	)
	defer func() {
		FinishBannerGenerateProgress(err)
	}()

	banners, err = s.generateAutoBannersWithConfig(ctx, repository.GetBannerConfig(), triggerSource)
	return banners, err
}

// generateAutoBannersWithConfig 开启刮削时缺额持续并发刮削至满额（最长不超过5分钟），关闭刮削时走常规抽取。
func (s *BannerAutoService) generateAutoBannersWithConfig(ctx context.Context, cfg model.BannerConfig, triggerSource string) (model.Banners, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	scrapeTimeout := 5 * time.Minute
	scrapeCtx, cancel := context.WithTimeout(ctx, scrapeTimeout)
	defer cancel()

	ReportBannerGenerateProgress(10, "正在加载当前排片配置与影片快照...")

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
		ReportBannerGenerateProgress(100, "置顶影片已满额，排片完成")
		return pinnedBanners, nil
	}

	ReportBannerGenerateProgress(15, "正在检索候选影片大池...")

	// 2. 动态过滤已在分类管理中设为不显示的分类：哪怕之前选中的时候显示，后面分类设置为不显示，依旧严格过滤
	effectiveCategories := repository.FilterShownCategoryIDs(cfg.Categories)
	if len(cfg.Categories) > 0 && len(effectiveCategories) == 0 {
		return nil, fmt.Errorf("所选分类已全部在分类管理中设置为不显示，无法排片，请重新选择排片分类")
	}
	if len(effectiveCategories) == 0 {
		// 未限定分类时，候选范围亦严格限定在当前所有显示的大类中，杜绝已隐藏分类（如体育/录像）渗透进轮播
		effectiveCategories = repository.GetShownRootCategoryIDs()
		if len(effectiveCategories) == 0 && db.Mdb != nil {
			return nil, fmt.Errorf("当前系统无任何处于显示状态的分类，无法排片，请在分类管理中开启至少一个分类")
		}
	}

	// 元素去重，防止重复分类输入导致配额失真及多次处理同一分类
	uniqueCats := make([]int64, 0, len(effectiveCategories))
	seenCatID := make(map[int64]struct{}, len(effectiveCategories))
	for _, cid := range effectiveCategories {
		if cid > 0 {
			if _, exists := seenCatID[cid]; !exists {
				seenCatID[cid] = struct{}{}
				uniqueCats = append(uniqueCats, cid)
			}
		}
	}
	effectiveCategories = uniqueCats

	// 2.1 针对所选分类按轮播需求总数计算配额，确保各分类均分（如10个轮播选2类，每类各5部）
	quotas := CalculateCategoryQuotas(effectiveCategories, needCount)

	// 2.2 收集当前已有轮播的影片 Mid，用于轮换去重（避免换来换去老是同一批）
	currentBanners := repository.GetBanners()
	currentMids := make(map[int64]struct{}, len(currentBanners))
	for _, b := range currentBanners {
		if b.Mid > 0 {
			currentMids[b.Mid] = struct{}{}
		}
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	freshByCat := make(map[int64][]model.FilmListSnapshot, len(effectiveCategories))
	existingByCat := make(map[int64][]model.FilmListSnapshot, len(effectiveCategories))
	var allFreshCandidates []model.FilmListSnapshot
	var allExistingCandidates []model.FilmListSnapshot
	totalCandidateCount := 0

	// 2.3 分别提取每个有效分类的候选影片池并随机打散与分流，杜绝某优势分类垄断候选
	// 刮削模式下池子扩大至 500 部，确保片库候选充足，避免因单次取样少而早早耗尽
	fetchPerCat := 150
	if canAutoTMDB {
		fetchPerCat = 500
	}
	for _, catPid := range effectiveCategories {
		catCands := filmsnapshot.GetSnapshotBannerCandidates(version, cfg.Strategy, []int64{catPid}, fetchPerCat)
		r.Shuffle(len(catCands), func(i, j int) {
			catCands[i], catCands[j] = catCands[j], catCands[i]
		})

		var fresh []model.FilmListSnapshot
		var existing []model.FilmListSnapshot
		for _, snap := range catCands {
			if snap.Mid <= 0 || strings.TrimSpace(snap.Name) == "" {
				continue
			}
			if _, isPinned := pinnedMap[snap.Mid]; isPinned {
				continue
			}
			if _, exists := currentMids[snap.Mid]; exists {
				existing = append(existing, snap)
			} else {
				fresh = append(fresh, snap)
			}
		}
		freshByCat[catPid] = fresh
		existingByCat[catPid] = existing
		allFreshCandidates = append(allFreshCandidates, fresh...)
		allExistingCandidates = append(allExistingCandidates, existing...)
		totalCandidateCount += len(fresh) + len(existing)
	}

	if totalCandidateCount == 0 {
		if len(cfg.Categories) > 0 {
			return nil, fmt.Errorf("所选分类下暂无候选影片，请调整排片分类或先采集影视数据")
		}
		return nil, fmt.Errorf("片库中无候选影片，请先采集影视数据")
	}

	log.Printf("[BannerAuto] 开始排片(策略=%s, 目标=%d部, 分类配额=%+v, 触发源=%s): 筛选出 %d 个候选影片 (全新候选: %d 个, 轮换已展: %d 个)",
		cfg.Strategy, targetCount, quotas, triggerSource, totalCandidateCount, len(allFreshCandidates), len(allExistingCandidates))
	ReportBannerGenerateProgress(20, fmt.Sprintf("已按分类筛选 %d 部候选影片，开始排片...", totalCandidateCount))

	var (
		pickedSnaps         []model.FilmListSnapshot
		reusedCount         int
		scrapedAttemptCount int
		scrapedSuccessCount int
	)

	if canAutoTMDB {
		// 3.1 开启 TMDB 自动刮削：针对各分类按配额持续并发刮削，严格保证每个类别的刮削与展示数量
		pickedSnaps, scrapedAttemptCount, scrapedSuccessCount, reusedCount = s.scrapeUntilTargetCount(
			scrapeCtx, needCount, effectiveCategories, quotas, freshByCat, existingByCat, version,
		)
	} else {
		// 3.2 未开启 TMDB 刮削：针对各分类按配额抽取可用影片，优先全新候选
		ReportBannerGenerateProgress(50, "未开启 TMDB 刮削，正在优选并均衡抽取各分类影片...")
		catPickedSnaps := make(map[int64][]model.FilmListSnapshot, len(effectiveCategories))
		totalPicked := 0

		for _, catPid := range effectiveCategories {
			catQuota := quotas[catPid]
			if catQuota <= 0 {
				continue
			}
			fresh := freshByCat[catPid]
			existing := existingByCat[catPid]
			var catPicks []model.FilmListSnapshot

			if len(fresh) > 0 {
				pickCount := catQuota
				if len(fresh) < pickCount {
					pickCount = len(fresh)
				}
				indices := r.Perm(len(fresh))
				for i := 0; i < pickCount; i++ {
					catPicks = append(catPicks, fresh[indices[i]])
				}
			}

			if len(catPicks) < catQuota && len(existing) > 0 {
				stillNeed := catQuota - len(catPicks)
				reusedIndices := r.Perm(len(existing))
				for i := 0; i < stillNeed && i < len(existing); i++ {
					catPicks = append(catPicks, existing[reusedIndices[i]])
					reusedCount++
				}
			}
			catPickedSnaps[catPid] = catPicks
			totalPicked += len(catPicks)
		}

		// 若个别分类片库候选不足配额导致总数不够，由其他分类有富余候选的补充
		if totalPicked < needCount {
			usedMidMap := make(map[int64]struct{})
			for _, picks := range catPickedSnaps {
				for _, p := range picks {
					usedMidMap[p.Mid] = struct{}{}
				}
			}
			for _, catPid := range effectiveCategories {
				if totalPicked >= needCount {
					break
				}
				for _, c := range append(freshByCat[catPid], existingByCat[catPid]...) {
					if totalPicked >= needCount {
						break
					}
					if _, used := usedMidMap[c.Mid]; !used && c.Mid > 0 {
						usedMidMap[c.Mid] = struct{}{}
						catPickedSnaps[catPid] = append(catPickedSnaps[catPid], c)
						totalPicked++
					}
				}
			}
		}

		// 交错交织各分类影片，保证首页大轮播分类均衡呈现
		pickedSnaps = InterleaveCategorySnapshots(effectiveCategories, catPickedSnaps)
	}

	// 3.3 兜底约束：候选库中必须至少有可用影片
	if len(pickedSnaps) == 0 && len(pinnedBanners) == 0 {
		return nil, fmt.Errorf("未能生成有效排片影片，请调整排片分类或先采集影视数据")
	}

	// 3.4 仅在未开启 TMDB 刮削的离线模式下，才对缺失横图的条目尝试用本地片库已有横图影片替换；开启刮削时完全以刮削结果为准，禁止回填替换破坏分类数量
	if !canAutoTMDB {
		missingSlideCount := 0
		for _, snap := range pickedSnaps {
			if strings.TrimSpace(snap.DisplayPictureSlide()) == "" {
				missingSlideCount++
			}
		}
		if missingSlideCount > 0 {
			var extraReused int
			pickedSnaps, extraReused = replaceMissingSlides(pickedSnaps, allFreshCandidates, allExistingCandidates, currentMids, r)
			reusedCount += extraReused

			// 如果普通候选池还不够，从全局快照表中查找拥有高清横图的影片做最终替换兜底 (严格限定在有效显示分类)
			pickedSnaps = replaceMissingSlidesWithGlobalHD(pickedSnaps, effectiveCategories, version)
		}
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
	ReportBannerGenerateProgress(95, "正在保存排片数据并刷新前台缓存...")
	if err := repository.SaveBanners(finalBanners); err != nil {
		return nil, fmt.Errorf("保存生成轮播失败: %w", err)
	}
	support.ClearIndexPageCache()

	scrapeInfo := "刮削: 0 个"
	if scrapedAttemptCount > 0 {
		scrapeInfo = fmt.Sprintf("刮削: %d 个 [有效成功: %d 个]", scrapedAttemptCount, scrapedSuccessCount)
	}
	log.Printf("[BannerAuto] 自动排片完成: 从 %d 个候选影片里面获取 %d 个轮播 (全新: %d 个, 本地已有横图: %d 个, %s, 复用旧轮播: %d 个, 置顶项: %d 个, 策略: %s, 原始配置分类: %v, 生效显示分类: %v, 触发源: %s)",
		totalCandidateCount, len(finalBanners), len(pickedSnaps)-reusedCount, localSlideCount, scrapeInfo, reusedCount, len(pinnedBanners), cfg.Strategy, cfg.Categories, effectiveCategories, triggerSource)
	return finalBanners, nil
}

