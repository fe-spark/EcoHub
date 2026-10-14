package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/repository/support"
	"server/internal/utils"
)

type BatchScrapeResult struct {
	TotalChecked int `json:"totalChecked"`
	MissingCount int `json:"missingCount"`
	SuccessCount int `json:"successCount"`
}

// BatchScrapeProgressEvent 轮播批量刮削 SSE 进度事件模型
type BatchScrapeProgressEvent struct {
	Event        string        `json:"event"`                  // "start" | "progress" | "done" | "error"
	TotalChecked int           `json:"totalChecked,omitempty"` // 总轮播检查数
	MissingCount int           `json:"missingCount,omitempty"` // 缺失横图待刮削数
	Current      int           `json:"current,omitempty"`      // 当前已处理完成数
	Mid          int64         `json:"mid,omitempty"`          // 当前影片 MID
	Name         string        `json:"name,omitempty"`         // 当前影片名称
	Success      bool          `json:"success,omitempty"`      // 是否成功匹配刮削
	Slide        string        `json:"slide,omitempty"`        // 刮削成功获取的横屏大图 URL
	SuccessCount int           `json:"successCount,omitempty"` // 最终累计成功数
	Banners      model.Banners `json:"banners,omitempty"`      // 最终最新的轮播完整列表
	Error        string        `json:"error,omitempty"`        // 错误提示
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

// BatchScrapeBannersStream 执行带 SSE 实时流式推送的轮播批量刮削
func (s *ManageService) BatchScrapeBannersStream(ctx context.Context, onEvent func(BatchScrapeProgressEvent)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	banners := repository.GetBanners()
	if len(banners) == 0 {
		if onEvent != nil {
			onEvent(BatchScrapeProgressEvent{
				Event:        "done",
				TotalChecked: 0,
				MissingCount: 0,
				SuccessCount: 0,
				Banners:      banners,
			})
		}
		return nil
	}

	tmdbCfg := repository.GetTMDBConfig()
	if !tmdbCfg.Enabled || strings.TrimSpace(tmdbCfg.ApiKey) == "" {
		err := fmt.Errorf("TMDB 刮削功能未启用或未配置 API Key，请先前往「系统设置 → 刮削配置」完成配置")
		if onEvent != nil {
			onEvent(BatchScrapeProgressEvent{Event: "error", Error: err.Error()})
		}
		return err
	}

	version := filmsnapshot.GetActiveSnapshotVersion()
	if version == "" {
		err := fmt.Errorf("当前系统无可用影片快照，请先采集影视数据")
		if onEvent != nil {
			onEvent(BatchScrapeProgressEvent{Event: "error", Error: err.Error()})
		}
		return err
	}

	// 找出所有缺失横图的影片 MID
	var missingMids []int64
	for _, b := range banners {
		if b.Mid <= 0 {
			continue
		}
		hasRealSlide := b.PictureSlide != "" && b.PictureSlide != b.Picture && b.PictureSlide != b.Poster
		if !hasRealSlide {
			missingMids = append(missingMids, b.Mid)
		}
	}

	if onEvent != nil {
		onEvent(BatchScrapeProgressEvent{
			Event:        "start",
			TotalChecked: len(banners),
			MissingCount: len(missingMids),
		})
	}

	if len(missingMids) == 0 {
		if onEvent != nil {
			onEvent(BatchScrapeProgressEvent{
				Event:        "done",
				TotalChecked: len(banners),
				MissingCount: 0,
				SuccessCount: 0,
				Banners:      s.GetBanners(),
			})
		}
		return nil
	}

	snaps := filmsnapshot.GetSnapshotsByMidsOrdered(version, missingMids)

	bannerMap := make(map[int64]model.Banner, len(banners))
	for _, b := range banners {
		if b.Mid > 0 {
			bannerMap[b.Mid] = b
		}
	}

	// 并发刮削：利用 semaphore 限制并发数为 4，全局硬超时 45s，单片超时 8s
	scrapeCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	var (
		mu                 sync.Mutex
		scrapedSuccessMids []int64
		processedCount     int
		wg                 sync.WaitGroup
		sem                = make(chan struct{}, 4)
	)

	for _, snap := range snaps {
		if scrapeCtx.Err() != nil {
			break
		}
		curSnap := snap
		// 若快照原始数据缺失年份或分类，融合当前轮播卡组中管理员配置的真实年份与分类
		if b, ok := bannerMap[curSnap.Mid]; ok {
			if curSnap.Year <= 0 && b.Year > 0 {
				curSnap.Year = b.Year
			}
			if curSnap.CName == "" && b.CName != "" {
				curSnap.CName = b.CName
			}
		}

		select {
		case sem <- struct{}{}:
		case <-scrapeCtx.Done():
			break
		}

		wg.Add(1)
		go func(s model.FilmListSnapshot) {
			defer func() {
				<-sem
				wg.Done()
			}()

			singleCtx, singleCancel := context.WithTimeout(scrapeCtx, 8*time.Second)
			defer singleCancel()

			updatedSnap, success, err := TMDBSvc.AutoScrapeFilm(singleCtx, s)
			var slideUrl string
			isSuccess := err == nil && success && updatedSnap != nil && updatedSnap.DisplayPictureSlide() != ""
			if isSuccess {
				slideUrl = updatedSnap.DisplayPictureSlide()
			}

			mu.Lock()
			processedCount++
			if isSuccess {
				scrapedSuccessMids = append(scrapedSuccessMids, updatedSnap.Mid)
			}
			curProcessed := processedCount
			mu.Unlock()

			if onEvent != nil {
				onEvent(BatchScrapeProgressEvent{
					Event:   "progress",
					Current: curProcessed,
					Mid:     s.Mid,
					Name:    s.Name,
					Success: isSuccess,
					Slide:   slideUrl,
				})
			}
		}(curSnap)
	}

	wg.Wait()

	var freshBanners model.Banners
	if len(scrapedSuccessMids) > 0 {
		_, _, _ = filmsnapshot.UpsertActiveSnapshotsByMids(scrapedSuccessMids...)
		support.ClearIndexPageCache()
		freshBanners = OverlayBannerLiveRemarks(banners)
		_ = repository.SaveBanners(freshBanners)
		log.Printf("[BatchScrapeBannersStream] 轮播批量刮削成功更新 %d 部影片横屏大图", len(scrapedSuccessMids))
	} else {
		freshBanners = s.GetBanners()
	}

	if onEvent != nil {
		onEvent(BatchScrapeProgressEvent{
			Event:        "done",
			TotalChecked: len(banners),
			MissingCount: len(missingMids),
			SuccessCount: len(scrapedSuccessMids),
			Banners:      freshBanners,
		})
	}

	return nil
}

// BatchScrapeBanners 为当前手动排片列表中缺失真实横屏幻灯大图的影片执行一键批量 TMDB 刮削
func (s *ManageService) BatchScrapeBanners(ctx context.Context) (BatchScrapeResult, error) {
	var result BatchScrapeResult
	err := s.BatchScrapeBannersStream(ctx, func(evt BatchScrapeProgressEvent) {
		if evt.Event == "start" {
			result.TotalChecked = evt.TotalChecked
			result.MissingCount = evt.MissingCount
		} else if evt.Event == "done" {
			result.SuccessCount = evt.SuccessCount
		}
	})
	return result, err
}
