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
) ([]model.FilmListSnapshot, int, int, int, error) {
	totalCandidateCount := 0
	for _, pid := range effectiveCategories {
		totalCandidateCount += len(freshByCat[pid]) + len(existingByCat[pid])
	}

	type catScrapeState struct {
		pid       int64
		quota     int
		pool      []model.FilmListSnapshot
		cursor    int
		attempts  int
		validList []model.FilmListSnapshot
	}

	states := make([]*catScrapeState, 0, len(effectiveCategories))
	for _, pid := range effectiveCategories {
		catQuota := quotas[pid]
		if catQuota <= 0 {
			continue
		}
		pool := make([]model.FilmListSnapshot, 0, len(freshByCat[pid])+len(existingByCat[pid]))
		pool = append(pool, freshByCat[pid]...)
		pool = append(pool, existingByCat[pid]...)
		states = append(states, &catScrapeState{
			pid:       pid,
			quota:     catQuota,
			pool:      pool,
			validList: make([]model.FilmListSnapshot, 0, catQuota),
		})
	}

	catValidSnaps := make(map[int64][]model.FilmListSnapshot, len(effectiveCategories))
	scrapedMids := make([]int64, 0, needCount)
	usedMids := make(map[int64]struct{}, totalCandidateCount)
	concurrency := 3
	attemptCount := 0
	successCount := 0
	consecutiveSysErrors := 0
	var fatalErr error

	calcTotalValid := func() int {
		total := 0
		for _, cs := range states {
			total += len(cs.validList)
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

	// 多分类轮询调度 (Round-Robin Fair Scheduling)：各分类轮流推进小批次刮削，杜绝某冷门分类独占超时导致后置分类饥饿
	for {
		if ctx.Err() != nil || fatalErr != nil {
			break
		}

		activeCategories := 0
		for _, cs := range states {
			if ctx.Err() != nil || fatalErr != nil {
				break
			}

			// 单分类尝试上限：配额的 6 倍，且至少尝试 20 部，最多 40 部（避免冷门分类死磕耗尽超时）
			maxAttemptsPerCat := cs.quota * 6
			if maxAttemptsPerCat < 20 {
				maxAttemptsPerCat = 20
			}
			if maxAttemptsPerCat > 40 {
				maxAttemptsPerCat = 40
			}

			// 分类已达标、片库候选耗尽或已达单类最大尝试预算
			if len(cs.validList) >= cs.quota || cs.cursor >= len(cs.pool) || cs.attempts >= maxAttemptsPerCat {
				continue
			}
			activeCategories++

			// 本轮针对该分类取一个小批次 (至多 3 部，小步快跑公平交错)
			batchSize := cs.quota - len(cs.validList)
			if batchSize > 3 {
				batchSize = 3
			}

			var toScrape []model.FilmListSnapshot
			for cs.cursor < len(cs.pool) && len(toScrape) < batchSize {
				cand := cs.pool[cs.cursor]
				cs.cursor++
				if _, used := usedMids[cand.Mid]; !used && cand.Mid > 0 {
					usedMids[cand.Mid] = struct{}{}
					toScrape = append(toScrape, cand)
				}
			}
			if len(toScrape) == 0 {
				continue
			}

			cs.attempts += len(toScrape)

			ReportBannerGenerateProgress(calcProgress(calcTotalValid()),
				fmt.Sprintf("正在从 TMDB 刮削各分类（已探测 %d, 已就绪 %d/%d 部)...", attemptCount, calcTotalValid(), needCount))

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
					log.Printf("[BannerAutoScrape] 影片刮削异常 (mid=%d): %v", res.original.Mid, res.err)
					errStr := res.err.Error()
					if strings.Contains(errStr, "401") || strings.Contains(errStr, "429") {
						fatalErr = res.err
					} else {
						consecutiveSysErrors++
						if consecutiveSysErrors >= 5 {
							fatalErr = fmt.Errorf("TMDB 外部服务连续多次请求失败 (网络不可达或代理故障): %w", res.err)
						}
					}
				} else {
					consecutiveSysErrors = 0
					if res.ok && res.updated != nil {
						successCount++
						scrapedMids = append(scrapedMids, res.updated.Mid)
						if len(cs.validList) < cs.quota {
							cs.validList = append(cs.validList, *res.updated)
							log.Printf("[BannerAutoScrape] 分类(pid=%d) 成功获取有效高清影片 mid=%d 片名=%q (当前类有效=%d/%d, 总就绪=%d/%d)",
								cs.pid, res.updated.Mid, res.updated.Name, len(cs.validList), cs.quota, calcTotalValid(), needCount)
						}
					}
				}
				ReportBannerGenerateProgress(calcProgress(calcTotalValid()),
					fmt.Sprintf("正在从 TMDB 刮削各分类（已探测 %d, 已就绪 %d/%d 部)...", attemptCount, calcTotalValid(), needCount))
			}
		}

		if activeCategories == 0 {
			break
		}
	}

	for _, cs := range states {
		catValidSnaps[cs.pid] = cs.validList
		log.Printf("[BannerAutoScrape] 分类(pid=%d) 刮削结束: 目标配额=%d, 探测尝试=%d, 实际获取=%d 部",
			cs.pid, cs.quota, cs.attempts, len(cs.validList))
	}

	// 轮播刮削凑额完成(或候选池耗尽/超时)，一次性统一批量发布快照与重建常驻搜索索引
	if len(scrapedMids) > 0 {
		ReportBannerGenerateProgress(88, fmt.Sprintf("正在批量更新并发布 %d 部影视快照...", len(scrapedMids)))
		log.Printf("[BannerAutoScrape] 轮播刮削完成，统一批量重建发布快照 mid_count=%d", len(scrapedMids))
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

	if fatalErr != nil {
		return pickedSnaps, attemptCount, successCount, reusedCount, fatalErr
	}

	return pickedSnaps, attemptCount, successCount, reusedCount, nil
}

// tryTMDBAutoScrape 直接获取去噪后的片名进行 TMDB 检索匹配，避免原始带杂质片名浪费额外的网络请求
func (s *BannerAutoService) tryTMDBAutoScrape(ctx context.Context, snap model.FilmListSnapshot, version string) (*model.FilmListSnapshot, bool, error) {
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

	log.Printf("[BannerAutoScrape] 发起去噪检索 mid=%d 关键词=%q (原名=%q)", snap.Mid, cleanedTarget, rawName)

	candidates, err := TMDBSvc.Search(cleanedTarget, "", "")
	if err != nil {
		log.Printf("[BannerAutoScrape] TMDB 请求异常 mid=%d 关键词=%q: %v", snap.Mid, cleanedTarget, err)
		return nil, false, err
	}

	if len(candidates) == 0 {
		log.Printf("[BannerAutoScrape] TMDB 未检索到任何候选条目 mid=%d 关键词=%q", snap.Mid, cleanedTarget)
		return nil, false, nil
	}

	var bestCandidate *model.TMDBCandidate
	bestScore := -1

	for i := range candidates {
		c := &candidates[i]
		hasPoster := strings.TrimSpace(c.Poster) != ""
		hasBackdrop := strings.TrimSpace(c.Backdrop) != ""
		if !hasPoster && !hasBackdrop {
			log.Printf("[BannerAutoScrape] 候选跳过(既无海报也无横图) id=%d 标题=%q", c.ID, c.Title)
			continue
		}

		cleanedCand, _ := CleanKeywordForSearch(c.Title)
		candTitle := strings.TrimSpace(c.Title)
		candOrig := strings.TrimSpace(c.OriginalTitle)
		exactTarget := strings.TrimSpace(cleanedTarget)

		mainCandTitle := extractMainTitle(candTitle)
		mainExactTarget := extractMainTitle(exactTarget)

		// 严格限制：只取名称一模一样的条目（支持去除原名括号后的主片名完全匹配）
		nameExact := strings.EqualFold(candTitle, exactTarget) ||
			strings.EqualFold(candOrig, exactTarget) ||
			(cleanedCand != "" && strings.EqualFold(cleanedCand, exactTarget)) ||
			strings.EqualFold(mainCandTitle, mainExactTarget) ||
			strings.EqualFold(candTitle, rawName) ||
			strings.EqualFold(candOrig, rawName)

		if !nameExact {
			log.Printf("[BannerAutoScrape] 候选跳过(名称未完全一致) id=%d 候选标题=%q 原名=%q 期望目标=%q",
				c.ID, c.Title, c.OriginalTitle, exactTarget)
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

		log.Printf("[BannerAutoScrape] 候选同名匹配命中(得分=%d) id=%d 标题=%q 年份=%s 类型=%s (海报=%t, 横图=%t, 当前最高=%d)",
			score, c.ID, c.Title, c.Year, c.MediaType, hasPoster, hasBackdrop, bestScore)

		if score > bestScore {
			bestScore = score
			bestCandidate = c
		}
	}

	if bestCandidate == nil {
		log.Printf("[BannerAutoScrape] 影片去噪刮削未匹配到有效 TMDB 条目 mid=%d 片名=%q 去噪片名=%q", snap.Mid, snap.Name, cleanedTarget)
		return nil, false, nil
	}

	matched := bestCandidate

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
