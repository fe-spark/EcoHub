package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/utils"
)

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
	AutoScrape bool            `json:"autoScrape"` // 生成时只预览匹配，替换线上轮播时才写入
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

	// 针对每个分类取策略排序下的前 N 部，已占用的影片不再进入后面的分类。
	catPicks := make(map[int64][]model.FilmListSnapshot, len(validQuotas))
	usedMids := make([]int64, 0, totalCount)
	for _, q := range validQuotas {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		effectiveCats := []int64{q.CategoryID}
		if sourceID != "" {
			mapped, fellBack := repository.ResolveBannerSourceCategories(sourceID, []int64{q.CategoryID})
			if fellBack || len(mapped) == 0 {
				log.Printf("[OneClickGenerateDeckStream] 分类 %d 不是当前采集站的一级分类，已跳过", q.CategoryID)
				catPicks[q.CategoryID] = nil
				continue
			}
			effectiveCats = mapped
		}
		cands := filmsnapshot.GetSnapshotBannerCandidates(version, strategy, sourceID, effectiveCats, q.Count, usedMids)
		catPicks[q.CategoryID] = cands
		for _, cand := range cands {
			if cand.Mid > 0 {
				usedMids = append(usedMids, cand.Mid)
			}
		}
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
		err := fmt.Errorf("未检索到可用影片")
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
		scrapeState := "none"
		if snap.IsScraped {
			scrapeState = "scraped"
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
			IsScraped:    snap.IsScraped,
			ScrapeState:  scrapeState,
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
				Message:  fmt.Sprintf("已选入: %s", snap.Name),
			})
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(30 * time.Millisecond):
		}
	}

	// 自动刮削只填充草稿预览，专属轮播使用，绝不改写片库
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
						Message: fmt.Sprintf("正在刮削 (%d 部)...", len(missingIndices)),
					})
				}

				for sIdx, bannerIdx := range missingIndices {
					select {
					case <-ctx.Done():
						return ctx.Err()
					default:
					}

					targetSnap := pickedSnaps[bannerIdx]
					singleCtx, singleCancel := context.WithTimeout(ctx, 8*time.Second)
					updatedSnap, mediaType, success, err := TMDBSvc.PreviewScrapeFilm(singleCtx, targetSnap)
					singleCancel()

					if ctx.Err() != nil {
						return ctx.Err()
					}

					if err == nil && success && updatedSnap != nil {
						slideUrl := strings.TrimSpace(updatedSnap.DisplayPictureSlide())
						dispPic := strings.TrimSpace(updatedSnap.DisplayPicture())
						if slideUrl == dispPic {
							slideUrl = ""
						}
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
						if updatedSnap.Year > 0 {
							banners[bannerIdx].Year = updatedSnap.Year
						}
						banners[bannerIdx].TmdbID = updatedSnap.TmdbID
						banners[bannerIdx].MediaType = mediaType
						banners[bannerIdx].PendingScrape = true
						banners[bannerIdx].ScrapeState = "pending"
						if onEvent != nil {
							onEvent(OneClickProgressEvent{
								Event:   "progress",
								Stage:   "scraping",
								Current: sIdx + 1,
								Total:   len(missingIndices),
								Mid:     targetSnap.Mid,
								Name:    targetSnap.Name,
								Slide:   slideUrl,
								Message: fmt.Sprintf("已匹配: %s", targetSnap.Name),
							})
						}
					} else {
						if banners[bannerIdx].ScrapeState != "scraped" {
							banners[bannerIdx].ScrapeState = "miss"
						}
						if onEvent != nil {
							onEvent(OneClickProgressEvent{
								Event:   "progress",
								Stage:   "scraping",
								Current: sIdx + 1,
								Total:   len(missingIndices),
								Mid:     targetSnap.Mid,
								Name:    targetSnap.Name,
								Message: fmt.Sprintf("未匹配: %s", targetSnap.Name),
							})
						}
					}
				}
			}
		}
	}

	for i := range banners {
		banners[i].Sort = int64(i + 1)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if onEvent != nil {
		onEvent(OneClickProgressEvent{
			Event:   "done",
			Stage:   "done",
			Current: len(banners),
			Total:   len(banners),
			Banners: banners,
			Message: fmt.Sprintf("生成完成（共 %d 部）", len(banners)),
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
