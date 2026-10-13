package snapshot

import (
	"slices"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/repository/support"
)

type relatedCandidateScore struct {
	Movie model.FilmListSnapshot
	Score int
}

func extractCoreSearchToken(name string) string {
	coreToken := strings.TrimSpace(name)
	if coreToken == "" {
		return ""
	}
	delimiters := []string{
		"：", ":", "·", " - ", "—", " ", "（", "(", "[", "【",
		"第", "剧场版", "大电影", "电影版", "真人版", "特别篇", "篇章", "番外", "彩蛋", "年番", "部", "季", "之",
	}
	minIdx := len(coreToken)
	for _, delimiter := range delimiters {
		if idx := strings.Index(coreToken, delimiter); idx > 0 && idx < minIdx {
			minIdx = idx
		}
	}
	if minIdx < len(coreToken) {
		coreToken = strings.TrimSpace(coreToken[:minIdx])
	}
	for _, suffix := range []string{"年番", "特别篇", "篇章", "番外", "彩蛋", "大电影", "电影版", "剧场版"} {
		coreToken = strings.TrimSpace(strings.TrimSuffix(coreToken, suffix))
	}
	runes := []rune(coreToken)
	if len(runes) >= 2 {
		return coreToken
	}
	nameRunes := []rune(strings.TrimSpace(name))
	if len(nameRunes) >= 2 {
		return string(nameRunes[:2])
	}
	return strings.TrimSpace(name)
}

func calcTitleScore(coreToken string, candidate model.FilmListSnapshot) int {
	coreToken = strings.TrimSpace(coreToken)
	if coreToken == "" {
		return 0
	}
	name := strings.TrimSpace(candidate.Name)
	subTitle := strings.TrimSpace(candidate.SubTitle)
	if name == coreToken {
		return 70
	}
	if strings.HasPrefix(name, coreToken) {
		return 60
	}
	if strings.Contains(name, coreToken) {
		return 30
	}
	if subTitle != "" && strings.Contains(subTitle, coreToken) {
		return 15
	}
	return 0
}

func calcTagOverlapScore(currentTags, candidateTags []string) int {
	if len(currentTags) == 0 || len(candidateTags) == 0 {
		return 0
	}
	currentSet := make(map[string]struct{}, len(currentTags))
	for _, t := range currentTags {
		currentSet[t] = struct{}{}
	}
	score := 0
	for _, tag := range candidateTags {
		if _, ok := currentSet[tag]; ok {
			score += 8
			if score >= 24 {
				return 24
			}
		}
	}
	return score
}

func calcMetaScore(current, candidate model.FilmListSnapshot) int {
	score := 0
	if current.Year > 0 && candidate.Year > 0 {
		diff := current.Year - candidate.Year
		if diff < 0 {
			diff = -diff
		}
		switch diff {
		case 0:
			score += 8
		case 1:
			score += 4
		}
	}
	if current.Area != "" && candidate.Area != "" && (current.Area == candidate.Area || (strings.Contains(current.Area, "大陆") && strings.Contains(candidate.Area, "大陆"))) {
		score += 5
	}
	if current.Language != "" && current.Language == candidate.Language {
		score += 3
	}
	return score
}

func freshnessBoost(stamp int64) int {
	if stamp <= 0 {
		return 0
	}
	age := time.Now().Unix() - stamp
	switch {
	case age <= 7*24*3600:
		return 10
	case age <= 30*24*3600:
		return 6
	case age <= 90*24*3600:
		return 3
	default:
		return 0
	}
}

func scoreRelatedCandidate(current, candidate model.FilmListSnapshot, coreToken string, currentTags []string) int {
	score := 0
	if current.SeriesKey != "" && current.SeriesKey == candidate.SeriesKey {
		score += 80
	}
	if (current.Cid > 0 && current.Cid == candidate.Cid) || (current.CategoryKey != "" && current.CategoryKey == candidate.CategoryKey) {
		score += 40
	}
	score += calcTitleScore(coreToken, candidate)
	score += calcTagOverlapScore(currentTags, extractDistinguishingTags(candidate.ClassTag))
	score += calcMetaScore(current, candidate)
	score += freshnessBoost(candidate.UpdateStamp)
	return score
}

func rankRelatedCandidates(current model.FilmListSnapshot, candidates []model.FilmListSnapshot, maxCandidates int) []model.FilmListSnapshot {
	if len(candidates) <= 1 {
		return candidates
	}
	coreToken := extractCoreSearchToken(current.Name)
	currentTags := extractDistinguishingTags(current.ClassTag)
	scored := make([]relatedCandidateScore, len(candidates))
	for i, c := range candidates {
		scored[i] = relatedCandidateScore{
			Movie: c,
			Score: scoreRelatedCandidate(current, c, coreToken, currentTags),
		}
	}
	slices.SortFunc(scored, func(a, b relatedCandidateScore) int {
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		if a.Movie.Hits != b.Movie.Hits {
			if a.Movie.Hits < b.Movie.Hits {
				return 1
			}
			return -1
		}
		if a.Movie.UpdateStamp != b.Movie.UpdateStamp {
			if a.Movie.UpdateStamp < b.Movie.UpdateStamp {
				return 1
			}
			return -1
		}
		if a.Movie.Mid < b.Movie.Mid {
			return 1
		}
		return -1
	})
	if len(scored) > maxCandidates {
		scored = scored[:maxCandidates]
	}
	res := make([]model.FilmListSnapshot, len(scored))
	for i, s := range scored {
		res[i] = s.Movie
	}
	return res
}

func extractDistinguishingTags(classTag string) []string {
	parts := strings.FieldsFunc(classTag, func(r rune) bool {
		return r == ',' || r == '/' || r == ' ' || r == '、' || r == '|' || r == ';'
	})
	stopWords := map[string]bool{
		"剧情": true, "其他": true, "未知": true, "普通话": true, "国语": true, "英语": true, "粤语": true, "原声": true,
		"动画": true, "动漫": true, "动画片": true, "电影": true, "电视剧": true, "综艺": true, "全集": true, "完结": true,
	}
	var res []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if len([]rune(p)) >= 2 && !stopWords[p] {
			res = append(res, p)
		}
	}
	return res
}
func resolveCurrentRootCategory(current model.FilmListSnapshot) (string, int64) {
	rootKey := strings.TrimSpace(current.RootCategoryKey)
	pid := current.Pid
	if pid <= 0 && current.CName != "" {
		pid = support.ResolveRootCategoryIDByCName(current.CName)
	}
	cName := strings.TrimSpace(current.CName)
	switch {
	case rootKey == "dongman" || pid == 4 || strings.Contains(cName, "动漫") || strings.Contains(cName, "动画"):
		return "dongman", 4
	case rootKey == "dianying" || pid == 1 || strings.Contains(cName, "电影") || strings.HasSuffix(cName, "片"):
		return "dianying", 1
	case rootKey == "dianshiju" || pid == 2 || strings.Contains(cName, "剧"):
		return "dianshiju", 2
	case rootKey == "zongyi" || pid == 3 || strings.Contains(cName, "综艺"):
		return "zongyi", 3
	}
	return rootKey, pid
}
