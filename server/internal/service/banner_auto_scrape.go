package service

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"server/internal/model"
	filmsnapshot "server/internal/repository/film/snapshot"
)

// scrapeUntilTargetCount 持续从候选池中并发刮削，直到达到目标数量 needCount 或 context 超时（最大5分钟）
func (s *BannerAutoService) scrapeUntilTargetCount(
	ctx context.Context,
	needCount int,
	freshCandidates []model.FilmListSnapshot,
	existingCandidates []model.FilmListSnapshot,
	categoryPids []int64,
	version string,
) ([]model.FilmListSnapshot, int, int, int) {
	pool := make([]model.FilmListSnapshot, 0, len(freshCandidates)+len(existingCandidates))
	pool = append(pool, freshCandidates...)
	pool = append(pool, existingCandidates...)

	validSnaps := make([]model.FilmListSnapshot, 0, needCount)
	scrapedMids := make([]int64, 0, needCount)
	usedMids := make(map[int64]struct{}, len(pool))
	fallbackCandidates := make([]model.FilmListSnapshot, 0, len(pool))
	cursor := 0
	concurrency := 3
	attemptCount := 0
	successCount := 0

	maxAttempts := needCount * 12
	if maxAttempts < 36 {
		maxAttempts = 36
	}
	if maxAttempts > 100 {
		maxAttempts = 100
	}
	if maxAttempts > len(pool) {
		maxAttempts = len(pool)
	}

	for len(validSnaps) < needCount && cursor < len(pool) {
		select {
		case <-ctx.Done():
			log.Printf("[BannerAutoScrape] 刮削达到超时预算上限或被中止，当前已收集 %d/%d 部有效高清影片，启动快速兜底补齐", len(validSnaps), needCount)
			break
		default:
		}
		if ctx.Err() != nil {
			break
		}
		if attemptCount >= maxAttempts {
			log.Printf("[BannerAutoScrape] 达到候选尝试上限(尝试=%d部, 有效=%d/%d)，停止继续检索外部接口，转入快速兜底补齐",
				attemptCount, len(validSnaps), needCount)
			break
		}

		shortfall := needCount - len(validSnaps)
		batchSize := shortfall
		if batchSize < 3 {
			batchSize = 3
		}
		if batchSize > 6 {
			batchSize = 6
		}

		var batch []model.FilmListSnapshot
		for cursor < len(pool) && len(batch) < batchSize {
			cand := pool[cursor]
			cursor++
			if _, used := usedMids[cand.Mid]; !used && cand.Mid > 0 {
				usedMids[cand.Mid] = struct{}{}
				batch = append(batch, cand)
			}
		}
		if len(batch) == 0 {
			break
		}

		var toScrape []model.FilmListSnapshot
		for _, item := range batch {
			if (item.IsCustomPicture || strings.TrimSpace(item.DisplayPictureSlide()) != "") && strings.TrimSpace(item.DisplayPicture()) != "" {
				validSnaps = append(validSnaps, item)
				log.Printf("[BannerAutoScrape] 影片已有高清素材/横屏大图 mid=%d 片名=%q，直接纳入有效排片 (当前有效=%d/%d)",
					item.Mid, item.Name, len(validSnaps), needCount)
				if len(validSnaps) >= needCount {
					break
				}
			} else {
				toScrape = append(toScrape, item)
			}
		}

		if len(validSnaps) >= needCount || len(toScrape) == 0 {
			continue
		}

		calcProgress := func(currAttempts int) int {
			pctByValid := float64(len(validSnaps)) / float64(needCount)
			pctByAttempts := float64(currAttempts) / float64(maxAttempts)
			ratio := pctByValid
			if pctByAttempts > ratio {
				ratio = pctByAttempts
			}
			p := 20 + int(ratio*65)
			if p > 85 {
				p = 85
			}
			return p
		}

		ReportBannerGenerateProgress(calcProgress(attemptCount), fmt.Sprintf("正在从 TMDB 检索（已探测 %d/%d, 已就绪 %d/%d 部)...", attemptCount, maxAttempts, len(validSnaps), needCount))

		type scrapeJobResult struct {
			original model.FilmListSnapshot
			updated  *model.FilmListSnapshot
			ok       bool
			err      error
		}

		jobs := make(chan model.FilmListSnapshot, len(toScrape))
		for _, item := range toScrape {
			jobs <- item
		}
		close(jobs)

		numWorkers := concurrency
		if len(toScrape) < numWorkers {
			numWorkers = len(toScrape)
		}

		results := make(chan scrapeJobResult, len(toScrape))
		var wg sync.WaitGroup

		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for item := range jobs {
					select {
					case <-ctx.Done():
						return
					default:
					}
					updated, ok, err := s.tryTMDBAutoScrape(ctx, item, version)
					results <- scrapeJobResult{original: item, updated: updated, ok: ok, err: err}
				}
			}()
		}

		go func() {
			wg.Wait()
			close(results)
		}()

		for res := range results {
			attemptCount++
			if res.err != nil {
				log.Printf("[BannerAutoScrape] 影片刮削记录异常 (已优雅降级): %v", res.err)
			}
			curPct := calcProgress(attemptCount)
			if res.ok && res.updated != nil {
				successCount++
				scrapedMids = append(scrapedMids, res.updated.Mid)
				if len(validSnaps) < needCount {
					validSnaps = append(validSnaps, *res.updated)
					log.Printf("[BannerAutoScrape] 成功获取有效高清影片 mid=%d 片名=%q (当前有效=%d/%d)",
						res.updated.Mid, res.updated.Name, len(validSnaps), needCount)
				}
				ReportBannerGenerateProgress(curPct, fmt.Sprintf("正在从 TMDB 检索（已探测 %d/%d, 已就绪 %d/%d 部)...", attemptCount, maxAttempts, len(validSnaps), needCount))
			} else {
				fallbackCandidates = append(fallbackCandidates, res.original)
				ReportBannerGenerateProgress(curPct, fmt.Sprintf("正在从 TMDB 检索（已探测 %d/%d, 已就绪 %d/%d 部)...", attemptCount, maxAttempts, len(validSnaps), needCount))
			}
		}
	}

	// 若片库耗尽或超时仍不足目标数量，优先从片库中已有高清横图/海报的优质影片兜底补足，绝不直接使用未匹配的低清小图
	if len(validSnaps) < needCount {
		ReportBannerGenerateProgress(85, fmt.Sprintf("正在从片库优选已有高清素材影片补足 %d 部轮播...", needCount))
		log.Printf("[BannerAutoScrape] 有效高清影片未满目标数量 (有效=%d, 目标=%d)，从片库优选已有高清大图的影片补足差额",
			len(validSnaps), needCount)

		// 1. 优先从全局快照中寻找已有高清横屏壁纸或自定义海报的影片 (严格遵循指定分类)
		hdSnaps := filmsnapshot.GetSnapshotHDBackdropCandidates(version, categoryPids, 50)
		for _, hd := range hdSnaps {
			if len(validSnaps) >= needCount {
				break
			}
			if _, used := usedMids[hd.Mid]; !used && hd.Mid > 0 {
				usedMids[hd.Mid] = struct{}{}
				validSnaps = append(validSnaps, hd)
				log.Printf("[BannerAutoScrape] 使用已有高清素材影片兜底 mid=%d 片名=%q", hd.Mid, hd.Name)
			}
		}

		// 2. 极端情况（全站片库无任何高清影片）：才使用普通候选保底
		for _, fb := range fallbackCandidates {
			if len(validSnaps) >= needCount {
				break
			}
			validSnaps = append(validSnaps, fb)
		}
		for cursor < len(pool) && len(validSnaps) < needCount {
			cand := pool[cursor]
			cursor++
			validSnaps = append(validSnaps, cand)
		}
	}

	// 轮播刮削凑额完成(或候选池耗尽)，一次性统一批量发布快照与重建常驻搜索索引
	if len(scrapedMids) > 0 {
		ReportBannerGenerateProgress(88, fmt.Sprintf("正在批量更新并发布 %d 部影视快照...", len(scrapedMids)))
		log.Printf("[BannerAutoScrape] 轮播刮削凑额完成(或候选池耗尽)，统一批量重建发布快照 mid_count=%d", len(scrapedMids))
		if _, _, err := filmsnapshot.UpsertActiveSnapshotsByMids(scrapedMids...); err != nil {
			log.Printf("[BannerAutoScrape] 批量发布快照失败: %v", err)
		}
	}

	reusedCount := 0
	existingMidMap := make(map[int64]struct{}, len(existingCandidates))
	for _, ec := range existingCandidates {
		existingMidMap[ec.Mid] = struct{}{}
	}
	for _, vs := range validSnaps {
		if _, exists := existingMidMap[vs.Mid]; exists {
			reusedCount++
		}
	}

	return validSnaps, attemptCount, successCount, reusedCount
}

// tryTMDBAutoScrape 先使用未经去噪的原始片名严格搜索匹配；若未命中，再使用去噪清洗后的名称二次检索匹配
func (s *BannerAutoService) tryTMDBAutoScrape(ctx context.Context, snap model.FilmListSnapshot, version string) (*model.FilmListSnapshot, bool, error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
	}

	rawName := strings.TrimSpace(snap.Name)
	cleanedTarget, extractedYear := CleanKeywordForSearch(rawName)
	snapYear := snap.Year
	if snapYear == 0 && extractedYear != "" {
		if y, err := strconv.ParseInt(extractedYear, 10, 64); err == nil {
			snapYear = y
		}
	}

	isTVCategory := strings.Contains(snap.CName, "剧") || strings.Contains(snap.CName, "动漫")
	isMovieCategory := strings.Contains(snap.CName, "影") || strings.Contains(snap.CName, "片")

	searchAndMatch := func(query string, targetName string, isRawPhase bool) (*model.TMDBCandidate, error) {
		query = strings.TrimSpace(query)
		if query == "" {
			return nil, nil
		}

		phaseLabel := "第一阶段:原始名称"
		if !isRawPhase {
			phaseLabel = "第二阶段:去噪名称"
		}

		log.Printf("[BannerAutoScrape] [%s] 发起检索 mid=%d 关键词=%q", phaseLabel, snap.Mid, query)

		var candidates []model.TMDBCandidate
		var err error
		if isRawPhase {
			candidates, err = TMDBSvc.SearchRaw(query, "", "")
		} else {
			candidates, err = TMDBSvc.Search(query, "", "")
		}
		if err != nil {
			log.Printf("[BannerAutoScrape] [%s] TMDB 请求异常 mid=%d 关键词=%q: %v", phaseLabel, snap.Mid, query, err)
			return nil, err
		}

		if len(candidates) == 0 {
			log.Printf("[BannerAutoScrape] [%s] TMDB 未检索到任何候选条目 mid=%d 关键词=%q", phaseLabel, snap.Mid, query)
			return nil, nil
		}

		var bestCandidate *model.TMDBCandidate
		bestScore := -1

		for i := range candidates {
			c := &candidates[i]
			hasPoster := strings.TrimSpace(c.Poster) != ""
			hasBackdrop := strings.TrimSpace(c.Backdrop) != ""
			if !hasPoster && !hasBackdrop {
				log.Printf("[BannerAutoScrape] [%s] 候选跳过(既无海报也无横图) id=%d 标题=%q",
					phaseLabel, c.ID, c.Title)
				continue
			}

			cleanedCand, _ := CleanKeywordForSearch(c.Title)
			candTitle := strings.TrimSpace(c.Title)
			candOrig := strings.TrimSpace(c.OriginalTitle)
			exactTarget := strings.TrimSpace(targetName)

			mainCandTitle := extractMainTitle(candTitle)
			mainExactTarget := extractMainTitle(exactTarget)

			// 严格限制：只取名称一模一样的条目（支持去除原名括号后的主片名完全匹配）
			nameExact := strings.EqualFold(candTitle, exactTarget) ||
				strings.EqualFold(candOrig, exactTarget) ||
				(cleanedCand != "" && strings.EqualFold(cleanedCand, exactTarget)) ||
				strings.EqualFold(mainCandTitle, mainExactTarget)

			if !nameExact {
				log.Printf("[BannerAutoScrape] [%s] 候选跳过(名称未完全一致) id=%d 候选标题=%q 原名=%q 期望目标=%q",
					phaseLabel, c.ID, c.Title, c.OriginalTitle, exactTarget)
				continue
			}

			score := 100
			if isTVCategory && c.MediaType == "tv" {
				score += 30
			} else if isMovieCategory && c.MediaType == "movie" {
				score += 30
			}

			// 年份非强制比对：采集无年份正常匹配；有年份相符(+0~1年)仅作为优选加分项，绝不强行过滤
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
				score += 20 // 高清封面
			}
			if hasBackdrop {
				score += 15 // 横屏海报（有更好，没有也可以）
			}

			score += int(c.VoteAverage * 2)

			log.Printf("[BannerAutoScrape] [%s] 候选同名匹配命中(得分=%d) id=%d 标题=%q 年份=%s 类型=%s (海报=%t, 横图=%t, 当前最高=%d)",
				phaseLabel, score, c.ID, c.Title, c.Year, c.MediaType, hasPoster, hasBackdrop, bestScore)

			if score > bestScore {
				bestScore = score
				bestCandidate = c
			}
		}

		return bestCandidate, nil
	}

	// 1. 第一阶段：先使用未经去噪的原始片名严格搜索匹配
	matched, err := searchAndMatch(rawName, rawName, true)
	if err != nil {
		return nil, false, err
	}

	// 2. 第二阶段：若原始名称未匹配到，且去噪后片名与原名不同，再使用去噪后片名二次检索匹配
	if matched == nil && cleanedTarget != "" && cleanedTarget != rawName {
		log.Printf("[BannerAutoScrape] 原始名称未匹配到条目，启动去噪后名称二次刮削 mid=%d 原名=%q 去噪后=%q",
			snap.Mid, rawName, cleanedTarget)
		matched, err = searchAndMatch(cleanedTarget, cleanedTarget, false)
		if err != nil {
			return nil, false, err
		}
	}

	if matched == nil {
		log.Printf("[BannerAutoScrape] 影片两阶段刮削均未匹配到有效 TMDB 条目 mid=%d 片名=%q", snap.Mid, snap.Name)
		return nil, false, nil
	}

	fields := []string{"poster"}
	if strings.TrimSpace(matched.Backdrop) != "" {
		fields = append(fields, "backdrop")
	}

	log.Printf("[BannerAutoScrape] 最终优选 TMDB 条目 id=%d 标题=%q (海报=%s, 横图=%s)，正在应用高清数据...",
		matched.ID, matched.Title, matched.Poster, matched.Backdrop)

	applyReq := model.TMDBApplyReq{
		Mid:       snap.Mid,
		TmdbID:    matched.ID,
		MediaType: matched.MediaType,
		Fields:    fields,
	}
	// 刮削阶段暂缓单片立即重建发布快照，落库后直接构造最新内存快照，待凑满目标数量后统一批量发布
	if _, err := TMDBSvc.ApplyDetailWithOptions(applyReq, false); err != nil {
		log.Printf("[BannerAutoScrape] TMDB 应用详情失败 mid=%d tmdbId=%d: %v", snap.Mid, matched.ID, err)
		return nil, false, err
	}

	updatedSnap := snap
	if strings.TrimSpace(matched.Poster) != "" {
		updatedSnap.Picture = strings.TrimSpace(matched.Poster)
		updatedSnap.CustomPicture = strings.TrimSpace(matched.Poster)
		updatedSnap.IsCustomPicture = true
	}
	if strings.TrimSpace(matched.Backdrop) != "" {
		updatedSnap.PictureSlide = strings.TrimSpace(matched.Backdrop)
		updatedSnap.CustomPictureSlide = strings.TrimSpace(matched.Backdrop)
	}

	log.Printf("[BannerAutoScrape] 影片 TMDB 高清数据已保存并就绪(待批量发布) mid=%d 片名=%q (封面=%s, 横图=%s)",
		snap.Mid, snap.Name, updatedSnap.DisplayPicture(), updatedSnap.DisplayPictureSlide())
	return &updatedSnap, true, nil
}
