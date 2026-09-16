package film

import (
	"regexp"
	"slices"
	"strings"
	"time"

	"server/internal/model"
)

type relatedCandidateScore struct {
	Movie model.FilmIndex
	Score int
}

func extractCoreSearchToken(name string) string {
	coreToken := strings.TrimSpace(name)
	if coreToken == "" {
		return ""
	}

	delimiters := []string{"：", ":", "·", " - ", "—", " ", "（", "(", "[", "【", "第", "剧场版", "部", "季", "之"}
	minIdx := len(coreToken)
	for _, delimiter := range delimiters {
		if idx := strings.Index(coreToken, delimiter); idx > 0 && idx < minIdx {
			minIdx = idx
		}
	}
	if minIdx < len(coreToken) {
		coreToken = strings.TrimSpace(coreToken[:minIdx])
	}
	coreToken = strings.TrimSpace(strings.TrimSuffix(coreToken, "年番"))
	for _, suffix := range []string{"特别篇", "篇章"} {
		coreToken = strings.TrimSpace(strings.TrimSuffix(coreToken, suffix))
	}
	for _, pattern := range []string{`(?i)tv\s*动画$`} {
		coreToken = strings.TrimSpace(regexp.MustCompile(pattern).ReplaceAllString(coreToken, ""))
	}

	runes := []rune(coreToken)
	nameRunes := []rune(strings.TrimSpace(name))
	if len(runes) >= 2 {
		return coreToken
	}
	if len(nameRunes) >= 4 {
		return string(nameRunes[:4])
	}
	if len(nameRunes) >= 2 {
		return string(nameRunes[:2])
	}
	return strings.TrimSpace(name)
}

func splitClassTags(classTag string) []string {
	normalized := strings.NewReplacer(" ", "", "/", ",", "|", ",", "，", ",").Replace(classTag)
	parts := strings.Split(normalized, ",")
	tags := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		tag := strings.TrimSpace(part)
		if tag == "" {
			continue
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	return tags
}

func splitAliasTitles(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := []string{raw}
	for _, sep := range []string{",", "，", "/", "|", "、"} {
		next := make([]string, 0, len(parts)*2)
		for _, part := range parts {
			if !strings.Contains(part, sep) {
				next = append(next, part)
				continue
			}
			for alias := range strings.SplitSeq(part, sep) {
				next = append(next, alias)
			}
		}
		parts = next
	}
	aliases := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		alias := strings.TrimSpace(part)
		if alias == "" {
			continue
		}
		if _, ok := seen[alias]; ok {
			continue
		}
		seen[alias] = struct{}{}
		aliases = append(aliases, alias)
	}
	return aliases
}

func buildTagSet(tags []string) map[string]struct{} {
	set := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		set[tag] = struct{}{}
	}
	return set
}

func calcTitleScore(coreToken string, candidate model.FilmIndex) int {
	coreToken = strings.TrimSpace(coreToken)
	if coreToken == "" {
		return 0
	}

	name := strings.TrimSpace(candidate.Name)
	subTitle := strings.TrimSpace(candidate.SubTitle)
	nameLike := strings.Contains(name, coreToken)
	prefixLike := strings.HasPrefix(name, coreToken)
	if name == coreToken {
		return 35
	}
	if prefixLike {
		return 25
	}
	if nameLike {
		return 15
	}
	if subTitle != "" && strings.Contains(subTitle, coreToken) {
		return 8
	}
	return 0
}

func calcAliasScore(current model.FilmIndex, candidate model.FilmIndex) int {
	aliases := splitAliasTitles(current.SubTitle)
	if len(aliases) == 0 {
		return 0
	}
	name := strings.TrimSpace(candidate.Name)
	subTitle := strings.TrimSpace(candidate.SubTitle)
	best := 0
	for _, alias := range aliases {
		score := 0
		switch {
		case alias == name:
			score = 20
		case strings.HasPrefix(name, alias):
			score = 14
		case strings.Contains(name, alias):
			score = 10
		case subTitle != "" && strings.Contains(subTitle, alias):
			score = 6
		}
		if score > best {
			best = score
		}
	}
	return best
}

func calcTagOverlapScore(currentTags, candidateTags []string) int {
	if len(currentTags) == 0 || len(candidateTags) == 0 {
		return 0
	}
	currentSet := buildTagSet(currentTags)
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

func calcMetaScore(current, candidate model.FilmIndex) int {
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
	if current.Area != "" && current.Area == candidate.Area {
		score += 5
	}
	if current.Language != "" && current.Language == candidate.Language {
		score += 3
	}
	return score
}

func freshnessBoost(candidate model.FilmIndex) int {
	stamp := candidate.UpdateStamp
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

func scoreRelatedCandidate(current model.FilmIndex, candidate model.FilmIndex) relatedCandidateScore {
	score := 0
	if current.SeriesKey != "" && current.SeriesKey == candidate.SeriesKey {
		score += 80
	}
	if current.Cid > 0 && current.Cid == candidate.Cid {
		score += 40
	}
	score += calcTitleScore(extractCoreSearchToken(current.Name), candidate)
	score += calcAliasScore(current, candidate)
	score += calcTagOverlapScore(splitClassTags(current.ClassTag), splitClassTags(candidate.ClassTag))
	score += calcMetaScore(current, candidate)
	score += freshnessBoost(candidate)
	return relatedCandidateScore{Movie: candidate, Score: score}
}

func rankRelatedCandidates(current model.FilmIndex, candidates []model.FilmIndex, pageSize int) []model.FilmIndex {
	if len(candidates) == 0 || pageSize <= 0 {
		return nil
	}
	scored := make([]relatedCandidateScore, 0, len(candidates))
	seen := make(map[int64]struct{}, len(candidates))
	for _, candidate := range candidates {
		if _, ok := seen[candidate.Mid]; ok {
			continue
		}
		seen[candidate.Mid] = struct{}{}
		scored = append(scored, scoreRelatedCandidate(current, candidate))
	}
	slices.SortFunc(scored, func(a, b relatedCandidateScore) int {
		if a.Score != b.Score {
			return b.Score - a.Score
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
		if a.Movie.Mid > b.Movie.Mid {
			return -1
		}
		return 0
	})
	if len(scored) > pageSize {
		scored = scored[:pageSize]
	}
	list := make([]model.FilmIndex, 0, len(scored))
	for _, item := range scored {
		list = append(list, item.Movie)
	}
	return list
}
