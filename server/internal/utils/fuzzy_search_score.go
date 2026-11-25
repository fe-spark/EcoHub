package utils

import (
	"strings"
)

type FilmSearchItem struct {
	Mid               int64
	Name              string
	CleanName         string
	PinyinFull        string
	PinyinInitials    string
	PinyinInitialAlts string
	Hits              int64
	Score             float64
	Year              int64
	UpdateStamp       int64
}

// FillSearchDerivedFields 建索引时预计算清洗文本与拼音。
func FillSearchDerivedFields(item *FilmSearchItem) {
	if item == nil {
		return
	}
	normName := TraditionalToSimplified(item.Name)
	item.CleanName = cleanCompactNormalized(normName)
	item.PinyinFull = pinyinFullNormalized(normName)
	variants := pinyinInitialVariantsNormalized(normName)
	if len(variants) > 0 {
		item.PinyinInitials = variants[0]
		if len(variants) > 1 {
			item.PinyinInitialAlts = strings.Join(variants[1:], " ")
		}
	}
}

// QueryContext 搜索词上下文
type QueryContext struct {
	RawKey      string
	CleanKey    string
	Tokens      []string
	CleanTokens []string
	CleanRunes  int
	IsAsciiOnly bool
}

// NameLikeNeedles 把中文搜索收成片名里必须出现的字。每组内部是或，组与组之间是与。
// 子序列和包含匹配都满足这些字；纯字母数字要靠拼音，收不成 LIKE。
func NameLikeNeedles(q QueryContext) [][]string {
	if q.IsAsciiOnly || q.CleanKey == "" {
		return nil
	}
	groups := make([][]string, 0, len([]rune(q.CleanKey)))
	seen := make(map[string]struct{})
	for _, r := range q.CleanKey {
		needle := string(r)
		if _, ok := seen[needle]; ok {
			continue
		}
		seen[needle] = struct{}{}
		base := []string{needle}
		if aliases, ok := numberAliases[needle]; ok {
			base = aliases
		}
		groups = append(groups, nameLikeForms(base))
	}
	return groups
}

// nameLikeForms 给每个针补上繁体和全角数字。片名库里两种写法都能进打分。
func nameLikeForms(needles []string) []string {
	seen := make(map[string]struct{}, len(needles))
	out := make([]string, 0, len(needles))
	add := func(s string) {
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, n := range needles {
		add(n)
		add(SimplifiedToTraditional(n))
		add(fullwidthDigits(n))
	}
	return out
}

func fullwidthDigits(s string) string {
	var b strings.Builder
	changed := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r + 0xFEE0)
			changed = true
			continue
		}
		b.WriteRune(r)
	}
	if !changed {
		return ""
	}
	return b.String()
}

// BuildQueryContext 预解析搜索词上下文，避免在循环匹配中重复计算
func BuildQueryContext(keyword string) QueryContext {
	raw := strings.TrimSpace(keyword)
	clean := CleanCompactText(raw)
	tokens := ExtractSearchTokens(raw)
	cleanTokens := make([]string, 0, len(tokens))
	seen := make(map[string]struct{}, len(tokens))
	for _, tok := range tokens {
		cTok := CleanCompactText(tok)
		if cTok == "" {
			continue
		}
		if _, ok := seen[cTok]; ok {
			continue
		}
		seen[cTok] = struct{}{}
		cleanTokens = append(cleanTokens, cTok)
	}

	return QueryContext{
		RawKey:      raw,
		CleanKey:    clean,
		Tokens:      tokens,
		CleanTokens: cleanTokens,
		CleanRunes:  len([]rune(clean)),
		IsAsciiOnly: IsAsciiAlphaNum(clean),
	}
}

func raiseScore(best, score int) int {
	if score > best {
		return score
	}
	return best
}

func resolveCleanField(clean, raw string) string {
	if clean != "" {
		return clean
	}
	if raw == "" {
		return ""
	}
	return CleanCompactText(raw)
}

func matchPinyinSyllables(syllables []string, cleanAsciiKey string) (int, bool) {
	if len(syllables) == 0 || cleanAsciiKey == "" {
		return 0, false
	}
	keyLen := len(cleanAsciiKey)
	n := len(syllables)
	for i := 0; i < n; i++ {
		pos := 0
		for j := i; j < n; j++ {
			syl := syllables[j]
			if syl == "" {
				continue
			}
			remain := keyLen - pos
			if remain <= 0 {
				break
			}
			if remain >= len(syl) {
				if cleanAsciiKey[pos:pos+len(syl)] != syl {
					break
				}
				pos += len(syl)
				if pos == keyLen {
					if i == 0 && j == n-1 {
						return 430, true
					}
					if i == 0 {
						s := 390 - (n-(j-i+1))*6
						if s < 340 {
							s = 340
						}
						return s, true
					}
					s := 350 - (n-(j-i+1))*6
					if s < 310 {
						s = 310
					}
					return s, true
				}
				continue
			}
			if i == 0 && j > 0 && strings.HasPrefix(syl, cleanAsciiKey[pos:]) {
				s := 360 - (len(syl)-remain)*3
				if s < 310 {
					s = 310
				}
				return s, true
			}
			break
		}
	}
	return 0, false
}

func scorePinyinMatch(item FilmSearchItem, cleanAsciiKey string) int {
	if cleanAsciiKey == "" {
		return 0
	}
	if !strings.HasPrefix(item.PinyinInitials, cleanAsciiKey) &&
		!strings.Contains(item.PinyinInitials, cleanAsciiKey) &&
		(item.PinyinInitialAlts == "" || !strings.Contains(item.PinyinInitialAlts, cleanAsciiKey)) &&
		item.PinyinFull != cleanAsciiKey &&
		!strings.HasPrefix(item.PinyinFull, cleanAsciiKey) &&
		!strings.Contains(item.PinyinFull, cleanAsciiKey) {
		return 0
	}
	best := 0
	nameForSyllables := item.CleanName
	if nameForSyllables == "" && item.Name != "" {
		nameForSyllables = CleanCompactText(item.Name)
	}
	if nameForSyllables != "" {
		syllables := pinyinSyllablesNormalized(nameForSyllables)
		if len(syllables) > 0 {
			if s, ok := matchPinyinSyllables(syllables, cleanAsciiKey); ok {
				best = raiseScore(best, s)
			}
		} else if item.PinyinFull != "" {
			if len(cleanAsciiKey) >= 3 && item.PinyinFull == cleanAsciiKey {
				best = raiseScore(best, 400)
			}
		}
	} else if item.PinyinFull != "" {
		if len(cleanAsciiKey) >= 3 && item.PinyinFull == cleanAsciiKey {
			best = raiseScore(best, 400)
		}
	}

	considerInitials := func(v string) {
		if v == "" {
			return
		}
		if len(cleanAsciiKey) >= 3 && v == cleanAsciiKey {
			best = raiseScore(best, 420)
			return
		}
		if len(cleanAsciiKey) >= 3 && strings.HasPrefix(v, cleanAsciiKey) {
			diff := len(v) - len(cleanAsciiKey)
			score := 380 - diff*6
			if score < 330 {
				score = 330
			}
			best = raiseScore(best, score)
		} else if len(cleanAsciiKey) >= 3 && strings.Contains(v, cleanAsciiKey) {
			diff := len(v) - len(cleanAsciiKey)
			score := 330 - diff*6
			if score < 300 {
				score = 300
			}
			best = raiseScore(best, score)
		}
	}
	considerInitials(item.PinyinInitials)
	if item.PinyinInitialAlts != "" {
		for _, v := range strings.Fields(item.PinyinInitialAlts) {
			considerInitials(v)
		}
	}

	return best
}

// ScoreFilmMatch 计算影视片名与搜索词的匹配度得分（0 表示不匹配）
func ScoreFilmMatch(item FilmSearchItem, q QueryContext) int {
	if q.CleanKey == "" {
		return 0
	}

	cleanName := resolveCleanField(item.CleanName, item.Name)
	nameRunes := len([]rune(cleanName))
	queryRunes := q.CleanRunes
	if queryRunes <= 0 {
		queryRunes = len([]rune(q.CleanKey))
	}

	bestScore := 0

	// 1. 片名完全匹配
	if cleanName == q.CleanKey {
		return 1000
	}

	// 2. 片名前缀或包含匹配
	if strings.HasPrefix(cleanName, q.CleanKey) {
		score := 850 - (nameRunes-queryRunes)*8
		if score < 750 {
			score = 750
		}
		bestScore = raiseScore(bestScore, score)
	} else if strings.Contains(cleanName, q.CleanKey) {
		score := 720 - (nameRunes-queryRunes)*8
		if score < 620 {
			score = 620
		}
		bestScore = raiseScore(bestScore, score)
	}

	// 3. 多 Token 分词匹配片名
	if len(q.CleanTokens) > 1 {
		nameHits := 0
		tokenLen := 0
		for _, tok := range q.CleanTokens {
			tokenLen += len([]rune(tok))
			if TokenMatchesText(tok, cleanName) {
				nameHits++
			}
		}
		if nameHits == len(q.CleanTokens) {
			score := 600 - (nameRunes-tokenLen)*6
			if score < 520 {
				score = 520
			}
			bestScore = raiseScore(bestScore, score)
		}
	}

	// 4. 片名子序列跳字匹配（如 "凡人传" -> "凡人修仙传"）
	if !q.IsAsciiOnly && queryRunes >= 3 && bestScore < 620 && IsSubsequence(q.CleanKey, cleanName) {
		score := 480 - (nameRunes-queryRunes)*6
		if score < 400 {
			score = 400
		}
		bestScore = raiseScore(bestScore, score)
	}

	// 5. 拼音匹配（全拼、首字母、多音字）
	if q.IsAsciiOnly {
		bestScore = raiseScore(bestScore, scorePinyinMatch(item, strings.ToLower(q.CleanKey)))
	}

	return bestScore
}
