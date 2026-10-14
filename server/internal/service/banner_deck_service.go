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
	MissingCount int           `json:"missingCount,omitempty"` // 待刮削影片数
	Current      int           `json:"current,omitempty"`      // 当前已处理完成数
	Mid          int64         `json:"mid,omitempty"`          // 当前影片 MID
	Name         string        `json:"name,omitempty"`         // 当前影片名称
	Success      bool          `json:"success,omitempty"`      // 是否成功匹配刮削
	Slide        string        `json:"slide,omitempty"`        // 刮削成功获取的横屏大图 URL
	SuccessCount int           `json:"successCount,omitempty"` // 最终累计成功数
	Banners      model.Banners `json:"banners,omitempty"`      // 最终最新的轮播完整列表
	Error        string        `json:"error,omitempty"`        // 错误提示
}

// OneClickProgressEvent 一键排片 SSE 进度事件模型
type OneClickProgressEvent struct {
	Event    string        `json:"event"`              // "start" | "progress" | "done" | "error"
	Stage    string        `json:"stage,omitempty"`    // "filtering" | "picking" | "saving" | "done"
	Current  int           `json:"current,omitempty"`  // 当前已装配影片序号
	Total    int           `json:"total,omitempty"`    // 目标排片数量
	Mid      int64         `json:"mid,omitempty"`      // 当前选定影片 MID
	Name     string        `json:"name,omitempty"`     // 当前选定影片名称
	Category string        `json:"category,omitempty"` // 所属分类名称
	Year     int64         `json:"year,omitempty"`     // 上映年份
	Poster   string        `json:"poster,omitempty"`   // 海报图片
	Slide    string        `json:"slide,omitempty"`    // 横屏大图
	Message  string        `json:"message,omitempty"`  // 进度提示文本
	Banners  model.Banners `json:"banners,omitempty"`  // 最终生效轮播卡组
	Error    string        `json:"error,omitempty"`    // 错误信息
}

// CategoryQuota 单个分类的排片数量配置
type CategoryQuota struct {
	CategoryID int64 `json:"categoryId"` // 分类ID
	Count      int   `json:"count"`      // 该分类计划排片数量
}

type OneClickGenerateReq struct {
	Strategy   string          `json:"strategy"`   // hot(最热), score(高分), latest(最新)
	Quotas     []CategoryQuota `json:"quotas"`     // 每个分类指定的排片配额列表
	AutoScrape bool            `json:"autoScrape"` // 是否自动执行 TMDB 全量元数据刮削
}

// OneClickGenerateDeckStream 一键排片：按指定分类配额与策略流式生成排片卡组（支持 SSE 实时事件流）
func (s *ManageService) OneClickGenerateDeckStream(ctx context.Context, req OneClickGenerateReq, onEvent func(OneClickProgressEvent)) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// 过滤有效分类配额并计算总数
	var validQuotas []CategoryQuota
	seenCat := make(map[int64]struct{})
	totalCount := 0
	for _, q := range req.Quotas {
		if q.CategoryID > 0 && q.Count > 0 {
			if _, exists := seenCat[q.CategoryID]; !exists {
				seenCat[q.CategoryID] = struct{}{}
				validQuotas = append(validQuotas, q)
				totalCount += q.Count
			}
		}
	}

	if totalCount <= 0 {
		err := fmt.Errorf("请至少为一个分类指定排片数量（大于 0）")
		if onEvent != nil {
			onEvent(OneClickProgressEvent{Event: "error", Error: err.Error()})
		}
		return err
	}

	if totalCount > model.MaxBannerCount {
		err := fmt.Errorf("排片总数不能超过上限（%d 部），当前已设定 %d 部", model.MaxBannerCount, totalCount)
		if onEvent != nil {
			onEvent(OneClickProgressEvent{Event: "error", Error: err.Error()})
		}
		return err
	}

	var strategy string
	strategyDesc := "最热优先"
	switch strings.ToLower(strings.TrimSpace(req.Strategy)) {
	case "score", "score_random":
		strategy = "score_random"
		strategyDesc = "评分优先"
	case "latest", "latest_random":
		strategy = "latest_random"
		strategyDesc = "最新更新"
	default:
		strategy = "hot_random"
		strategyDesc = "最热优先"
	}

	if onEvent != nil {
		onEvent(OneClickProgressEvent{
			Event:   "start",
			Stage:   "filtering",
			Current: 0,
			Total:   totalCount,
			Message: fmt.Sprintf("正在从片库检索候选影片（策略：%s，共 %d 个分类，目标 %d 部）...", strategyDesc, len(validQuotas), totalCount),
		})
	}

	version := filmsnapshot.GetActiveSnapshotVersion()
	if version == "" {
		err := fmt.Errorf("当前系统无可用影片快照，请先采集影视数据")
		if onEvent != nil {
			onEvent(OneClickProgressEvent{Event: "error", Error: err.Error()})
		}
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	sourceID := ""
	if active := repository.GetActiveCollectSource(); active != nil {
		sourceID = strings.TrimSpace(active.Id)
	}

	// 针对每个分类单独提取指定数量的候选影片
	catPicks := make(map[int64][]model.FilmListSnapshot, len(validQuotas))
	for _, q := range validQuotas {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		effectiveCats := []int64{q.CategoryID}
		if sourceID != "" {
			mapped, _ := repository.ResolveBannerSourceCategories(sourceID, []int64{q.CategoryID})
			if len(mapped) > 0 {
				effectiveCats = mapped
			}
		}
		cands := filmsnapshot.GetSnapshotBannerCandidates(version, strategy, sourceID, effectiveCats, q.Count)
		catPicks[q.CategoryID] = cands
	}

	// 按 Round-Robin 交错穿插排列各分类抽选结果
	maxLen := 0
	for _, list := range catPicks {
		if len(list) > maxLen {
			maxLen = len(list)
		}
	}

	var pickedSnaps []model.FilmListSnapshot
	for idx := 0; idx < maxLen; idx++ {
		for _, q := range validQuotas {
			list := catPicks[q.CategoryID]
			if idx < len(list) {
				pickedSnaps = append(pickedSnaps, list[idx])
			}
		}
	}

	if len(pickedSnaps) == 0 {
		err := fmt.Errorf("所选分类与配额下未检索到可用影片，请调整条件或先采集影视数据")
		if onEvent != nil {
			onEvent(OneClickProgressEvent{Event: "error", Error: err.Error()})
		}
		return err
	}

	// 逐部装配轮播并推送进度
	banners := make(model.Banners, 0, len(pickedSnaps))
	for i, snap := range pickedSnaps {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		slide := strings.TrimSpace(snap.DisplayPictureSlide())
		pic := strings.TrimSpace(snap.DisplayPicture())
		if slide == pic {
			slide = ""
		}
		b := model.Banner{
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
		}
		banners = append(banners, b)

		if onEvent != nil {
			onEvent(OneClickProgressEvent{
				Event:    "progress",
				Stage:    "picking",
				Current:  i + 1,
				Total:    len(pickedSnaps),
				Mid:      snap.Mid,
				Name:     snap.Name,
				Category: snap.CName,
				Year:     snap.Year,
				Poster:   pic,
				Slide:    slide,
				Message:  fmt.Sprintf("已选入: %s (%s)", snap.Name, snap.CName),
			})
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(30 * time.Millisecond):
		}
	}

	// 若用户启用了自动刮削，对未刮削或缺失横屏大图的影片进行 TMDB 智能全量刮削
	if req.AutoScrape {
		tmdbCfg := repository.GetTMDBConfig()
		if tmdbCfg.Enabled && strings.TrimSpace(tmdbCfg.ApiKey) != "" {
			var missingIndices []int
			for idx, b := range banners {
				hasRealSlide := b.PictureSlide != "" && b.PictureSlide != b.Picture && b.PictureSlide != b.Poster
				if !b.IsScraped || !hasRealSlide {
					missingIndices = append(missingIndices, idx)
				}
			}

			if len(missingIndices) > 0 {
				if onEvent != nil {
					onEvent(OneClickProgressEvent{
						Event:   "progress",
						Stage:   "scraping",
						Current: 0,
						Total:   len(missingIndices),
						Message: fmt.Sprintf("正在对 %d 部影片执行 TMDB 自动刮削...", len(missingIndices)),
					})
				}

				var scrapedMids []int64
				for sIdx, bannerIdx := range missingIndices {
					select {
					case <-ctx.Done():
						return ctx.Err()
					default:
					}

					targetSnap := pickedSnaps[bannerIdx]
					singleCtx, singleCancel := context.WithTimeout(ctx, 8*time.Second)
					updatedSnap, success, err := TMDBSvc.AutoScrapeFilm(singleCtx, targetSnap)
					singleCancel()

					if err == nil && success && updatedSnap != nil {
						slideUrl := updatedSnap.DisplayPictureSlide()
						dispPic := updatedSnap.DisplayPicture()
						if dispPic != "" {
							banners[bannerIdx].Picture = dispPic
							banners[bannerIdx].Poster = dispPic
						}
						banners[bannerIdx].PictureSlide = slideUrl
						if updatedSnap.Score > 0 {
							banners[bannerIdx].Score = updatedSnap.Score
						}
						if updatedSnap.Blurb != "" {
							banners[bannerIdx].Blurb = updatedSnap.Blurb
						}
						if updatedSnap.Actor != "" {
							banners[bannerIdx].Actor = updatedSnap.Actor
						}
						if updatedSnap.Director != "" {
							banners[bannerIdx].Director = updatedSnap.Director
						}
						if updatedSnap.Year > 0 {
							banners[bannerIdx].Year = updatedSnap.Year
						}
						if updatedSnap.ClassTag != "" {
							banners[bannerIdx].ClassTag = updatedSnap.ClassTag
						}
						banners[bannerIdx].IsScraped = true
						banners[bannerIdx].TmdbID = updatedSnap.TmdbID

						scrapedMids = append(scrapedMids, updatedSnap.Mid)
						if onEvent != nil {
							onEvent(OneClickProgressEvent{
								Event:    "progress",
								Stage:    "scraping",
								Current:  sIdx + 1,
								Total:    len(missingIndices),
								Mid:      targetSnap.Mid,
								Name:     targetSnap.Name,
								Slide:    slideUrl,
								Message:  fmt.Sprintf("TMDB 全量刮削成功: %s", targetSnap.Name),
							})
						}
					} else {
						if onEvent != nil {
							onEvent(OneClickProgressEvent{
								Event:    "progress",
								Stage:    "scraping",
								Current:  sIdx + 1,
								Total:    len(missingIndices),
								Mid:      targetSnap.Mid,
								Name:     targetSnap.Name,
								Message:  fmt.Sprintf("TMDB 未能匹配元数据: %s", targetSnap.Name),
							})
						}
					}
				}

				if len(scrapedMids) > 0 {
					_, _, _ = filmsnapshot.UpsertActiveSnapshotsByMids(scrapedMids...)
					banners = OverlayBannerLiveRemarks(banners)
					log.Printf("[OneClickGenerateDeckStream] 一键排片自动刮削更新 %d 部影片全量元数据", len(scrapedMids))
				}
			}
		}
	}

	for i := range banners {
		banners[i].Sort = int64(i + 1)
	}

	if onEvent != nil {
		onEvent(OneClickProgressEvent{
			Event:   "done",
			Stage:   "done",
			Current: len(banners),
			Total:   len(banners),
			Banners: banners,
			Message: fmt.Sprintf("一键排片方案生成完毕，共包含 %d 部影片（待确认后替换线上）", len(banners)),
		})
	}

	return nil
}

// OneClickGenerateDeck 一键排片同步调用方法
func (s *ManageService) OneClickGenerateDeck(req OneClickGenerateReq) (model.Banners, error) {
	var finalBanners model.Banners
	err := s.OneClickGenerateDeckStream(context.Background(), req, func(evt OneClickProgressEvent) {
		if evt.Event == "done" && evt.Banners != nil {
			finalBanners = evt.Banners
		}
	})
	if err != nil {
		return nil, err
	}
	if finalBanners == nil {
		finalBanners = s.GetBanners()
	}
	return finalBanners, nil
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

	// 找出所有未刮削或缺失横图的影片 MID
	var missingMids []int64
	for _, b := range banners {
		if b.Mid <= 0 {
			continue
		}
		hasRealSlide := b.PictureSlide != "" && b.PictureSlide != b.Picture && b.PictureSlide != b.Poster
		if !b.IsScraped || !hasRealSlide {
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

scrapeLoop:
	for _, snap := range snaps {
		if scrapeCtx.Err() != nil {
			break scrapeLoop
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
			break scrapeLoop
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
			isSuccess := err == nil && success && updatedSnap != nil
			if isSuccess && updatedSnap.DisplayPictureSlide() != "" {
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
		freshBanners = OverlayBannerLiveRemarks(banners)
		log.Printf("[BatchScrapeBannersStream] 轮播批量刮削成功更新 %d 部影片全量元数据（待发布生效）", len(scrapedSuccessMids))
	} else {
		freshBanners = banners
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
