package service

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/repository/support"
	"server/internal/utils"
)

type BatchScrapeResult struct {
	TotalChecked int `json:"totalChecked"`
	MissingCount int `json:"missingCount"`
	SuccessCount int `json:"successCount"`
}

type OneClickGenerateReq struct {
	Count      int     `json:"count"`      // 排片数量 (1-12)
	Strategy   string  `json:"strategy"`   // hot(最热), score(高分), latest(最新)
	Categories []int64 `json:"categories"` // 筛选分类ID列表
}

// OneClickGenerateDeck 一键排片：按选择分类与策略（最热/高分/最新）快速从片库生成排片卡组
func (s *ManageService) OneClickGenerateDeck(req OneClickGenerateReq) (model.Banners, error) {
	count := req.Count
	if count <= 0 {
		count = 6
	}
	if count > model.MaxBannerCount {
		count = model.MaxBannerCount
	}

	var strategy string
	switch strings.ToLower(strings.TrimSpace(req.Strategy)) {
	case "score", "score_random":
		strategy = "score_random"
	case "latest", "latest_random":
		strategy = "latest_random"
	default:
		strategy = "hot_random"
	}

	version := filmsnapshot.GetActiveSnapshotVersion()
	if version == "" {
		return nil, fmt.Errorf("当前系统无可用影片快照，请先采集影视数据")
	}

	sourceID := ""
	if active := repository.GetActiveCollectSource(); active != nil {
		sourceID = strings.TrimSpace(active.Id)
	}

	var effectiveCategories []int64
	if len(req.Categories) > 0 {
		if sourceID != "" {
			effectiveCategories, _ = repository.ResolveBannerSourceCategories(sourceID, req.Categories)
		} else {
			effectiveCategories = repository.FilterShownCategoryIDs(req.Categories)
		}
	}
	if len(effectiveCategories) == 0 {
		if sourceID != "" {
			effectiveCategories, _ = repository.ResolveBannerSourceCategories(sourceID, nil)
		}
		if len(effectiveCategories) == 0 {
			effectiveCategories = repository.GetShownRootCategoryIDs()
		}
	}

	// 分类去重
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

	var pickedSnaps []model.FilmListSnapshot

	if len(effectiveCategories) <= 1 {
		// 单分类或无分类限定：直接取 Top 影片
		pickedSnaps = filmsnapshot.GetSnapshotBannerCandidates(version, strategy, sourceID, effectiveCategories, count)
	} else {
		// 多分类均分抽取：每个分类计算配额并交错排列
		numCats := len(effectiveCategories)
		perCat := count / numCats
		rem := count % numCats

		catPicks := make(map[int64][]model.FilmListSnapshot, numCats)
		totalPicked := 0
		for i, cid := range effectiveCategories {
			quota := perCat
			if i < rem {
				quota++
			}
			if quota <= 0 {
				continue
			}
			cands := filmsnapshot.GetSnapshotBannerCandidates(version, strategy, sourceID, []int64{cid}, quota)
			catPicks[cid] = cands
			totalPicked += len(cands)
		}

		// 按 Round-Robin 交错穿插排列各分类影片
		maxLen := 0
		for _, list := range catPicks {
			if len(list) > maxLen {
				maxLen = len(list)
			}
		}
		for idx := 0; idx < maxLen; idx++ {
			for _, cid := range effectiveCategories {
				list := catPicks[cid]
				if idx < len(list) && len(pickedSnaps) < count {
					pickedSnaps = append(pickedSnaps, list[idx])
				}
			}
		}

		// 若由于个别分类片源少导致总数不足，从其他分类备选中补充
		if len(pickedSnaps) < count {
			usedMap := make(map[int64]struct{}, len(pickedSnaps))
			for _, p := range pickedSnaps {
				usedMap[p.Mid] = struct{}{}
			}
			for _, cid := range effectiveCategories {
				if len(pickedSnaps) >= count {
					break
				}
				for _, p := range filmsnapshot.GetSnapshotBannerCandidates(version, strategy, sourceID, []int64{cid}, count) {
					if len(pickedSnaps) >= count {
						break
					}
					if _, used := usedMap[p.Mid]; !used && p.Mid > 0 {
						usedMap[p.Mid] = struct{}{}
						pickedSnaps = append(pickedSnaps, p)
					}
				}
			}
		}
	}

	if len(pickedSnaps) == 0 {
		return nil, fmt.Errorf("所选分类与策略下未检索到可用影片，请调整条件或先采集影视数据")
	}

	banners := make(model.Banners, 0, len(pickedSnaps))
	for i, snap := range pickedSnaps {
		slide := strings.TrimSpace(snap.DisplayPictureSlide())
		pic := strings.TrimSpace(snap.DisplayPicture())
		banners = append(banners, model.Banner{
			Id:           utils.GenerateSalt(),
			Mid:          snap.Mid,
			Name:         snap.Name,
			Year:         snap.Year,
			CName:        snap.CName,
			Poster:       pic,
			Picture:      pic,
			PictureSlide: slide,
			Remark:       snap.Remarks,
			Sort:         int64(i + 1),
			IsCustomPic:  false,
		})
	}

	if err := repository.SaveBanners(banners); err != nil {
		return nil, fmt.Errorf("保存一键排片失败: %w", err)
	}
	support.ClearIndexPageCache()

	return s.GetBanners(), nil
}

// SaveBannerDeck 保存手动排片卡组列表与排序
func (s *ManageService) SaveBannerDeck(bl model.Banners) error {
	if len(bl) > model.MaxBannerCount {
		return fmt.Errorf("排片影片数量不能超过上限（%d 部）", model.MaxBannerCount)
	}
	for i := range bl {
		bl[i].Sort = int64(i + 1)
	}
	return s.SaveBanners(bl)
}

// BatchScrapeBanners 为当前手动排片列表中缺失真实横屏幻灯大图的影片执行一键批量 TMDB 刮削
func (s *ManageService) BatchScrapeBanners(ctx context.Context) (BatchScrapeResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	banners := repository.GetBanners()
	result := BatchScrapeResult{
		TotalChecked: len(banners),
	}
	if len(banners) == 0 {
		return result, nil
	}

	tmdbCfg := repository.GetTMDBConfig()
	if !tmdbCfg.Enabled || strings.TrimSpace(tmdbCfg.ApiKey) == "" {
		return result, fmt.Errorf("TMDB 刮削功能未启用或未配置 API Key，请先前往「系统设置 → 刮削配置」完成配置")
	}

	version := filmsnapshot.GetActiveSnapshotVersion()
	if version == "" {
		return result, fmt.Errorf("当前系统无可用影片快照，请先采集影视数据")
	}

	// 找出所有缺失横图的影片 MID
	var missingMids []int64
	for _, b := range banners {
		if b.Mid <= 0 {
			continue
		}
		// 无横图或横图仅为竖图兜底
		hasRealSlide := b.PictureSlide != "" && b.PictureSlide != b.Picture && b.PictureSlide != b.Poster
		if !hasRealSlide {
			missingMids = append(missingMids, b.Mid)
		}
	}

	result.MissingCount = len(missingMids)
	if len(missingMids) == 0 {
		return result, nil
	}

	snaps := filmsnapshot.GetSnapshotsByMidsOrdered(version, missingMids)
	scrapedSuccessMids := make([]int64, 0, len(snaps))

	scrapeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	for _, snap := range snaps {
		if scrapeCtx.Err() != nil {
			break
		}
		updatedSnap, success, err := scrapeSingleFilmTMDB(scrapeCtx, snap)
		if err == nil && success && updatedSnap != nil && updatedSnap.DisplayPictureSlide() != "" {
			result.SuccessCount++
			scrapedSuccessMids = append(scrapedSuccessMids, updatedSnap.Mid)
		}
	}

	if len(scrapedSuccessMids) > 0 {
		// 增量发布快照，保证前台与后续读取即刻生效
		_, _, _ = filmsnapshot.UpsertActiveSnapshotsByMids(scrapedSuccessMids...)

		// 同步回填当前排片列表中的横图
		liveData := filmrepo.LiveBannerSnapshotsByMIDs(scrapedSuccessMids)
		for i := range banners {
			if snap, ok := liveData[banners[i].Mid]; ok {
				if slide := snap.DisplayPictureSlide(); slide != "" {
					banners[i].PictureSlide = slide
				}
			}
		}
		_ = repository.SaveBanners(banners)
		support.ClearIndexPageCache()
		log.Printf("[BatchScrapeBanners] 一键批量刮削成功更新 %d 部影片横版大图", len(scrapedSuccessMids))
	}

	return result, nil
}

// scrapeSingleFilmTMDB 针对单个影片进行 TMDB 智能匹配并写入元数据
func scrapeSingleFilmTMDB(ctx context.Context, snap model.FilmListSnapshot) (*model.FilmListSnapshot, bool, error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
	}

	rawName := strings.TrimSpace(snap.Name)
	cleanedTarget, extractedYear := CleanKeywordForSearch(rawName)
	if cleanedTarget == "" {
		cleanedTarget = rawName
	}
	snapYear := snap.Year
	if snapYear == 0 && extractedYear != "" {
		if y, err := strconv.ParseInt(extractedYear, 10, 64); err == nil {
			snapYear = y
		}
	}

	isTVCategory := strings.Contains(snap.CName, "剧") || strings.Contains(snap.CName, "动漫")
	isMovieCategory := strings.Contains(snap.CName, "影") || strings.Contains(snap.CName, "片")

	candidates, err := TMDBSvc.Search(cleanedTarget, "", "")
	if err != nil {
		log.Printf("[BannerScrape] TMDB 请求异常 mid=%d 关键词=%q: %v", snap.Mid, cleanedTarget, err)
		return nil, false, err
	}

	if len(candidates) == 0 {
		return nil, false, nil
	}

	var bestCandidate *model.TMDBCandidate
	bestScore := -1

	for i := range candidates {
		c := &candidates[i]
		hasPoster := strings.TrimSpace(c.Poster) != ""
		hasBackdrop := strings.TrimSpace(c.Backdrop) != ""
		if !hasPoster && !hasBackdrop {
			continue
		}

		cleanedCand, _ := CleanKeywordForSearch(c.Title)
		candTitle := strings.TrimSpace(c.Title)
		candOrig := strings.TrimSpace(c.OriginalTitle)
		exactTarget := strings.TrimSpace(cleanedTarget)

		mainCandTitle := extractCleanTitle(candTitle)
		mainExactTarget := extractCleanTitle(exactTarget)

		nameExact := strings.EqualFold(candTitle, exactTarget) ||
			strings.EqualFold(candOrig, exactTarget) ||
			(cleanedCand != "" && strings.EqualFold(cleanedCand, exactTarget)) ||
			strings.EqualFold(mainCandTitle, mainExactTarget) ||
			strings.EqualFold(candTitle, rawName) ||
			strings.EqualFold(candOrig, rawName)

		if !nameExact {
			continue
		}

		score := 100
		if isTVCategory && c.MediaType == "tv" {
			score += 30
		} else if isMovieCategory && c.MediaType == "movie" {
			score += 30
		}

		if snapYear > 0 && c.Year != "" {
			candY, _ := strconv.Atoi(c.Year)
			if candY > 0 {
				diff := int(snapYear) - candY
				if diff == 0 {
					score += 30
				} else if diff == 1 || diff == -1 {
					score += 15
				}
			}
		}

		if hasPoster {
			score += 20
		}
		if hasBackdrop {
			score += 30 // 横版大图高优先级加权
		}
		score += int(c.VoteAverage * 2)

		if score > bestScore {
			bestScore = score
			bestCandidate = c
		}
	}

	if bestCandidate == nil {
		return nil, false, nil
	}

	fields := []string{"poster"}
	if strings.TrimSpace(bestCandidate.Backdrop) != "" {
		fields = append(fields, "backdrop")
	}

	applyReq := model.TMDBApplyReq{
		Mid:       snap.Mid,
		TmdbID:    bestCandidate.ID,
		MediaType: bestCandidate.MediaType,
		Fields:    fields,
	}

	if _, err := TMDBSvc.ApplyDetailWithOptions(applyReq, false); err != nil {
		log.Printf("[BannerScrape] TMDB 应用详情失败 mid=%d tmdbId=%d: %v", snap.Mid, bestCandidate.ID, err)
		return nil, false, err
	}

	updatedSnap := snap
	if strings.TrimSpace(bestCandidate.Poster) != "" {
		updatedSnap.Picture = strings.TrimSpace(bestCandidate.Poster)
		updatedSnap.CustomPicture = strings.TrimSpace(bestCandidate.Poster)
		updatedSnap.IsCustomPicture = true
	}
	if strings.TrimSpace(bestCandidate.Backdrop) != "" {
		updatedSnap.PictureSlide = strings.TrimSpace(bestCandidate.Backdrop)
		updatedSnap.CustomPictureSlide = strings.TrimSpace(bestCandidate.Backdrop)
	}

	log.Printf("[BannerScrape] 影片 TMDB 高清数据已保存 mid=%d 片名=%q (横图=%s)",
		snap.Mid, snap.Name, updatedSnap.DisplayPictureSlide())
	return &updatedSnap, true, nil
}

func extractCleanTitle(s string) string {
	s = strings.TrimSpace(s)
	for _, sep := range []string{"（", "(", "【", "["} {
		if idx := strings.Index(s, sep); idx > 0 {
			s = strings.TrimSpace(s[:idx])
		}
	}
	return s
}
