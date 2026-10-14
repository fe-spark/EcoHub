package service

import (
	"context"
	"log"
	"regexp"
	"strconv"
	"strings"

	"server/internal/model"
)

// AutoScrapeFilm 对单部影片执行智能匹配与 TMDB 元数据自动刮削落库
func (s *TMDBService) AutoScrapeFilm(ctx context.Context, snap model.FilmListSnapshot) (*model.FilmListSnapshot, bool, error) {
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

	candidates, err := s.Search(cleanedTarget, "", "")
	if err != nil {
		log.Printf("[TMDBAuto] TMDB 检索异常 mid=%d 关键词=%q: %v", snap.Mid, cleanedTarget, err)
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

	if _, err := s.ApplyDetailWithOptions(applyReq, false); err != nil {
		log.Printf("[TMDBAuto] 应用 TMDB 详情失败 mid=%d tmdbId=%d: %v", snap.Mid, bestCandidate.ID, err)
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

	log.Printf("[TMDBAuto] 影片 TMDB 高清数据已自动刮削落库 mid=%d 片名=%q (横图=%s)",
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


