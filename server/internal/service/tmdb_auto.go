package service

import (
	"context"
	"errors"
	"log"
	"regexp"
	"strconv"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/repository/support"
)

type ScrapeResult struct {
	Total   int `json:"total"`
	Success int `json:"success"`
	Failed  int `json:"failed"`
}

// ScrapeFilms 对影片执行 TMDB 自动匹配与元数据刮削落库（兼容单个与批量）
func (s *TMDBService) ScrapeFilms(ctx context.Context, mids []int64) (*ScrapeResult, error) {
	cfg := repository.GetTMDBConfig()
	if !cfg.Enabled || strings.TrimSpace(cfg.ApiKey) == "" {
		return nil, errors.New("尚未配置或未启用 TMDB 刮削功能")
	}

	cleanMids := make([]int64, 0, len(mids))
	seen := make(map[int64]struct{}, len(mids))
	for _, id := range mids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		cleanMids = append(cleanMids, id)
	}

	res := &ScrapeResult{Total: len(cleanMids)}
	if len(cleanMids) == 0 {
		return res, nil
	}

	var successMids []int64
	for _, mid := range cleanMids {
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		default:
		}

		snap := filmsnapshot.GetSnapshotByMid("", mid)
		if snap == nil {
			var index model.FilmIndex
			if err := db.Mdb.Where("mid = ?", mid).First(&index).Error; err != nil {
				res.Failed++
				continue
			}
			snap = filmsnapshot.GetSnapshotByMid("", mid)
			if snap == nil {
				res.Failed++
				continue
			}
		}

		_, ok, err := s.AutoScrapeFilm(ctx, *snap)
		if err != nil || !ok {
			res.Failed++
		} else {
			res.Success++
			successMids = append(successMids, mid)
		}
	}

	if len(successMids) > 0 {
		_, _, _ = filmsnapshot.UpsertActiveSnapshotsByMids(successMids...)
		filmsnapshot.ClearDynamicPlayCaches()
		support.ClearIndexPageCache()
	}

	return res, nil
}

// AutoScrapeFilm 对单部影片执行智能匹配与 TMDB 元数据自动刮削落库
func (s *TMDBService) AutoScrapeFilm(ctx context.Context, snap model.FilmListSnapshot) (*model.FilmListSnapshot, bool, error) {
	updated, _, ok, err := s.autoScrapeFilm(ctx, snap, true)
	return updated, ok, err
}

// PreviewScrapeFilm 只匹配并组装预览，不写刮削表，不刷新线上快照。
func (s *TMDBService) PreviewScrapeFilm(ctx context.Context, snap model.FilmListSnapshot) (*model.FilmListSnapshot, string, bool, error) {
	return s.autoScrapeFilm(ctx, snap, false)
}

func (s *TMDBService) autoScrapeFilm(ctx context.Context, snap model.FilmListSnapshot, persist bool) (*model.FilmListSnapshot, string, bool, error) {
	select {
	case <-ctx.Done():
		return nil, "", false, ctx.Err()
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

	candidates, err := s.Search(cleanedTarget, "", "")
	if err != nil {
		log.Printf("[TMDBAuto] TMDB 检索异常 mid=%d 关键词=%q: %v", snap.Mid, cleanedTarget, err)
		return nil, "", false, err
	}

	// Search 本身不接收 context，进行中的 HTTP 会跑完。返回后若已取消，不再落库。
	select {
	case <-ctx.Done():
		return nil, "", false, ctx.Err()
	default:
	}

	if len(candidates) == 0 {
		return nil, "", false, nil
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

		normCandTitle := normalizeComparableTitle(candTitle)
		normCandOrig := normalizeComparableTitle(candOrig)
		normExactTarget := normalizeComparableTitle(exactTarget)
		normRawName := normalizeComparableTitle(rawName)
		normCleanedCand := normalizeComparableTitle(cleanedCand)
		normMainCandTitle := normalizeComparableTitle(mainCandTitle)
		normMainExactTarget := normalizeComparableTitle(mainExactTarget)

		nameExact := strings.EqualFold(candTitle, exactTarget) ||
			strings.EqualFold(candOrig, exactTarget) ||
			(cleanedCand != "" && strings.EqualFold(cleanedCand, exactTarget)) ||
			strings.EqualFold(mainCandTitle, mainExactTarget) ||
			strings.EqualFold(candTitle, rawName) ||
			strings.EqualFold(candOrig, rawName) ||
			(normCandTitle != "" && (normCandTitle == normExactTarget || normCandTitle == normRawName)) ||
			(normCandOrig != "" && (normCandOrig == normExactTarget || normCandOrig == normRawName)) ||
			(normCleanedCand != "" && normCleanedCand == normExactTarget) ||
			(normMainCandTitle != "" && normMainCandTitle == normMainExactTarget)

		// 相似度计算（容许两岸三地译名微调，如“功夫男孩.拉面大乱” vs “功夫小子～拉面大乱斗”）
		simTitle := calculateTitleSimilarity(normCandTitle, normExactTarget)
		if simRaw := calculateTitleSimilarity(normCandTitle, normRawName); simRaw > simTitle {
			simTitle = simRaw
		}
		if simCandOrig := calculateTitleSimilarity(normCandOrig, normExactTarget); simCandOrig > simTitle {
			simTitle = simCandOrig
		}

		// 检查年份是否相符
		candY, _ := strconv.Atoi(c.Year)
		yearExact := false
		yearClose := false
		if snapYear > 0 && candY > 0 {
			diff := int(snapYear) - candY
			if diff == 0 {
				yearExact = true
				yearClose = true
			} else if diff == 1 || diff == -1 {
				yearClose = true
			}
		}

		// 匹配通过条件：
		// 1. 完全精确匹配（原有相等或标点归一化完全相等）
		// 2. 年份吻合 (diff <= 1) 且文本相似度 >= 0.70 (覆盖两岸译名、副标题微调)
		// 3. 极高相似度 >= 0.75 (即便快照年份缺失，核心长文本匹配也可安全放行)
		// 4. TMDB 检索结果唯一且相似度 >= 0.60 且年份吻合（或未指定年份）
		isMatch := nameExact ||
			(simTitle >= 0.70 && yearClose) ||
			(simTitle >= 0.75) ||
			(len(candidates) == 1 && simTitle >= 0.60 && (yearClose || snapYear == 0))

		if !isMatch {
			continue
		}

		score := int(simTitle * 100)
		if nameExact {
			score += 40
		}

		isAnimeCategory := strings.Contains(snap.CName, "动漫") || strings.Contains(snap.CName, "动画")
		if isAnimeCategory && (c.MediaType == "movie" || c.MediaType == "tv") {
			score += 25
		} else if isTVCategory && c.MediaType == "tv" {
			score += 30
		} else if isMovieCategory && c.MediaType == "movie" {
			score += 30
		}

		if yearExact {
			score += 30
		} else if yearClose {
			score += 15
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
		return nil, "", false, nil
	}

	select {
	case <-ctx.Done():
		return nil, "", false, ctx.Err()
	default:
	}

	if !persist {
		updated := previewSnapshotFromCandidate(snap, bestCandidate)
		return &updated, bestCandidate.MediaType, true, nil
	}

	applyReq := model.TMDBApplyReq{
		Mid:       snap.Mid,
		TmdbID:    bestCandidate.ID,
		MediaType: bestCandidate.MediaType,
		Fields:    nil, // nil 表示全量应用所有字段（海报、横图、评分、演职员、简介、年份等）
	}

	if _, err := s.ApplyDetailWithOptions(applyReq, false); err != nil {
		log.Printf("[TMDBAuto] 应用 TMDB 详情失败 mid=%d tmdbId=%d: %v", snap.Mid, bestCandidate.ID, err)
		return nil, "", false, err
	}

	updatedSnap := snap
	if stored := filmsnapshot.GetSnapshotByMid("", snap.Mid); stored != nil {
		updatedSnap = *stored
	}
	updatedSnap.IsScraped = true
	if updatedSnap.TmdbID <= 0 {
		updatedSnap.TmdbID = bestCandidate.ID
	}

	log.Printf("[TMDBAuto] 影片 TMDB 全量元数据已自动刮削落库 mid=%d 片名=%q (评分=%.1f, 横图=%s)",
		snap.Mid, snap.Name, updatedSnap.Score, updatedSnap.DisplayPictureSlide())
	return &updatedSnap, bestCandidate.MediaType, true, nil
}

func previewSnapshotFromCandidate(snap model.FilmListSnapshot, c *model.TMDBCandidate) model.FilmListSnapshot {
	updated := snap
	updated.TmdbID = c.ID
	if poster := strings.TrimSpace(c.Poster); poster != "" {
		updated.Picture = poster
	}
	if slide := strings.TrimSpace(c.Backdrop); slide != "" && slide != strings.TrimSpace(updated.Picture) {
		updated.PictureSlide = slide
	}
	if c.VoteAverage > 0 {
		updated.Score = c.VoteAverage
	}
	if overview := strings.TrimSpace(c.Overview); overview != "" {
		updated.Blurb = overview
		updated.Content = overview
	}
	if y, err := strconv.Atoi(strings.TrimSpace(c.Year)); err == nil && y > 0 {
		updated.Year = int64(y)
	}
	return updated
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

var titlePunctuationRegex = regexp.MustCompile(`[\s\p{P}\p{S}\x{3000}]+`)

// normalizeComparableTitle 移除标题中的所有标点符号、特殊符号与空白字符，并转小写用于语义去噪匹配
func normalizeComparableTitle(s string) string {
	return strings.ToLower(titlePunctuationRegex.ReplaceAllString(strings.TrimSpace(s), ""))
}

// calculateTitleSimilarity 计算两个归一化标题的相似度（基于 LCS 最长公共子序列比例，范围 0.0 ~ 1.0）
func calculateTitleSimilarity(s1, s2 string) float64 {
	r1 := []rune(s1)
	r2 := []rune(s2)
	l1 := len(r1)
	l2 := len(r2)
	if l1 == 0 || l2 == 0 {
		return 0
	}
	if s1 == s2 {
		return 1.0
	}

	maxLen := l1
	if l2 > maxLen {
		maxLen = l2
	}
	minLen := l1
	if l2 < minLen {
		minLen = l2
	}

	// 动态规划计算 LCS
	dp := make([]int, l2+1)
	for i := 1; i <= l1; i++ {
		prev := 0
		for j := 1; j <= l2; j++ {
			temp := dp[j]
			if r1[i-1] == r2[j-1] {
				dp[j] = prev + 1
			} else if dp[j-1] > dp[j] {
				dp[j] = dp[j-1]
			}
			prev = temp
		}
	}
	lcs := dp[l2]

	simMin := float64(lcs) / float64(minLen)
	simMax := float64(lcs) / float64(maxLen)
	return simMin*0.4 + simMax*0.6
}
