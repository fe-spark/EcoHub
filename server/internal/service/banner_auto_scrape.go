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

// scrapeUntilTargetCount 持续针对各分类按配额并发刮削，保证每个所选分类刮削展示的数量均衡，直到达到总额或 context 超时（最大5分钟）
// 不回填之前片库已有的历史刮削数据兜底，纯粹依赖本次实时刮削，数量不够就一直按批次尝试刮削，直至达标或超时
func (s *BannerAutoService) scrapeUntilTargetCount(
	ctx context.Context,
	needCount int,
	effectiveCategories []int64,
	quotas map[int64]int,
	freshByCat map[int64][]model.FilmListSnapshot,
	existingByCat map[int64][]model.FilmListSnapshot,
	version string,
) ([]model.FilmListSnapshot, int, int, int) {
	totalCandidateCount := 0
	for _, pid := range effectiveCategories {
		totalCandidateCount += len(freshByCat[pid]) + len(existingByCat[pid])
	}

	catValidSnaps := make(map[int64][]model.FilmListSnapshot, len(effectiveCategories))
	scrapedMids := make([]int64, 0, needCount)
	usedMids := make(map[int64]struct{}, totalCandidateCount)
	concurrency := 3
	attemptCount := 0
	successCount := 0

	calcTotalValid := func() int {
		total := 0
		for _, snaps := range catValidSnaps {
			total += len(snaps)
		}
		return total
	}

	calcProgress := func(currValid int) int {
		pctByValid := float64(currValid) / float64(needCount)
		p := 20 + int(pctByValid*65)
		if p > 85 {
			p = 85
		}
		return p
	}

	// 依次为各分类按照专属配额进行刮削，严格保证每个类别的有效数量
	for _, catPid := range effectiveCategories {
		if ctx.Err() != nil {
			break
		}
		catQuota := quotas[catPid]
		if catQuota <= 0 {
			continue
		}

		catPool := make([]model.FilmListSnapshot, 0, len(freshByCat[catPid])+len(existingByCat[catPid]))
		catPool = append(catPool, freshByCat[catPid]...)
		catPool = append(catPool, existingByCat[catPid]...)

		cursor := 0
		validForThisCat := make([]model.FilmListSnapshot, 0, catQuota)

		// 数量不够就一直执行刮削，直到达到配额、候选池遍历完毕或 context 超时
		for len(validForThisCat) < catQuota && cursor < len(catPool) {
			select {
			case <-ctx.Done():
				log.Printf("[BannerAutoScrape] 刮削达到超时预算上限或被中止，分类(pid=%d)已收集 %d/%d 部",
					catPid, len(validForThisCat), catQuota)
				break
			default:
			}
			if ctx.Err() != nil {
				break
			}

			shortfall := catQuota - len(validForThisCat)
			batchSize := shortfall
			if batchSize < 3 {
				batchSize = 3
			}
			if batchSize > 6 {
				batchSize = 6
			}

			var toScrape []model.FilmListSnapshot
			for cursor < len(catPool) && len(toScrape) < batchSize {
				cand := catPool[cursor]
				cursor++
				if _, used := usedMids[cand.Mid]; !used && cand.Mid > 0 {
					usedMids[cand.Mid] = struct{}{}
					toScrape = append(toScrape, cand)
				}
			}
			if len(toScrape) == 0 {
				break
			}

			curTotalValid := calcTotalValid() + len(validForThisCat)
			ReportBannerGenerateProgress(calcProgress(curTotalValid),
				fmt.Sprintf("正在从 TMDB 刮削各分类（已探测 %d, 已就绪 %d/%d 部)...", attemptCount, curTotalValid, needCount))

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
				if res.ok && res.updated != nil {
					successCount++
					scrapedMids = append(scrapedMids, res.updated.Mid)
					if len(validForThisCat) < catQuota {
						validForThisCat = append(validForThisCat, *res.updated)
						log.Printf("[BannerAutoScrape] 分类(pid=%d) 成功获取有效高清影片 mid=%d 片名=%q (当前类有效=%d/%d, 总就绪=%d/%d)",
							catPid, res.updated.Mid, res.updated.Name, len(validForThisCat), catQuota, calcTotalValid()+len(validForThisCat), needCount)
					}
				}
				curTotal := calcTotalValid() + len(validForThisCat)
				ReportBannerGenerateProgress(calcProgress(curTotal),
					fmt.Sprintf("正在从 TMDB 刮削各分类（已探测 %d, 已就绪 %d/%d 部)...", attemptCount, curTotal, needCount))
			}
		}

		catValidSnaps[catPid] = validForThisCat
		log.Printf("[BannerAutoScrape] 分类(pid=%d) 配额刮削完毕: 目标配额=%d, 实际刮削获取=%d 部",
			catPid, catQuota, len(validForThisCat))
	}

	// 轮播刮削凑额完成(或候选池耗尽/超时)，一次性统一批量发布快照与重建常驻搜索索引
	if len(scrapedMids) > 0 {
		ReportBannerGenerateProgress(88, fmt.Sprintf("正在批量更新并发布 %d 部影视快照...", len(scrapedMids)))
		log.Printf("[BannerAutoScrape] 轮播刮削凑额完成，统一批量重建发布快照 mid_count=%d", len(scrapedMids))
		if _, _, err := filmsnapshot.UpsertActiveSnapshotsByMids(scrapedMids...); err != nil {
			log.Printf("[BannerAutoScrape] 批量发布快照失败: %v", err)
		}
	}

	// 各分类按 Round-Robin 交错排列合并，保证首页展示均衡多样
	pickedSnaps := InterleaveCategorySnapshots(effectiveCategories, catValidSnaps)

	// 统计复用的旧轮播影片数量
	reusedCount := 0
	existingMidMap := make(map[int64]struct{})
	for _, pid := range effectiveCategories {
		for _, ec := range existingByCat[pid] {
			existingMidMap[ec.Mid] = struct{}{}
		}
	}
	for _, vs := range pickedSnaps {
		if _, exists := existingMidMap[vs.Mid]; exists {
			reusedCount++
		}
	}

	return pickedSnaps, attemptCount, successCount, reusedCount
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
