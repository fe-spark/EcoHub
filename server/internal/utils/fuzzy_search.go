package utils

import (
	"strings"
	"unicode"
)

var numberAliases = map[string][]string{
	"1":   {"1", "一", "i", "第一", "第1"},
	"2":   {"2", "二", "ii", "第二", "第2", "两"},
	"3":   {"3", "三", "iii", "第三", "第3"},
	"4":   {"4", "四", "iv", "第四", "第4"},
	"5":   {"5", "五", "v", "第五", "第5"},
	"6":   {"6", "六", "vi", "第六", "第6"},
	"7":   {"7", "七", "vii", "第七", "第7"},
	"8":   {"8", "八", "viii", "第八", "第8"},
	"9":   {"9", "九", "ix", "第九", "第9"},
	"10":  {"10", "十", "x", "第十", "第10"},
	"一":   {"一", "1", "i", "第一", "第1"},
	"二":   {"二", "2", "ii", "第二", "第2", "两"},
	"三":   {"三", "3", "iii", "第三", "第3"},
	"四":   {"四", "4", "iv", "第四", "第4"},
	"五":   {"五", "5", "v", "第五", "第5"},
	"六":   {"六", "6", "vi", "第六", "第6"},
	"七":   {"七", "7", "vii", "第七", "第7"},
	"八":   {"八", "8", "viii", "第八", "第8"},
	"九":   {"九", "9", "ix", "第九", "第9"},
	"十":   {"十", "10", "x", "第十", "第10"},
	"i":   {"i", "1", "一"},
	"ii":  {"ii", "2", "二"},
	"iii": {"iii", "3", "三"},
	"iv":  {"iv", "4", "四"},
	"v":   {"v", "5", "五"},
}

// NormalizeSearchText 归一化搜索文本：繁简转换、全角转半角、小写、标点替换为空格并压缩空格
func NormalizeSearchText(s string) string {
	s = TraditionalToSimplified(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		// 全角英数与常见全角符号转半角
		if r >= 0xFF01 && r <= 0xFF5E {
			r = r - 0xFEE0
		} else if r == 0x3000 { // 全角空格
			r = ' '
		}

		if r < 128 {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
			} else if r >= 'A' && r <= 'Z' {
				b.WriteRune(r + ('a' - 'A'))
			} else {
				b.WriteByte(' ')
			}
			continue
		}

		if unicode.Is(unicode.Han, r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}

	fields := strings.Fields(b.String())
	return strings.Join(fields, " ")
}

// CleanCompactText 提取紧凑纯文本（去空格、标点、全小写、繁转简）
// 例如："哈利·波特 与 魔法石 2" -> "哈利波特与魔法石2"
func CleanCompactText(s string) string {
	return cleanCompactNormalized(TraditionalToSimplified(s))
}

func cleanCompactNormalized(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		if r >= 0xFF01 && r <= 0xFF5E {
			r = r - 0xFEE0
		}
		if r < 128 {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
			} else if r >= 'A' && r <= 'Z' {
				b.WriteRune(r + ('a' - 'A'))
			}
			continue
		}
		if unicode.Is(unicode.Han, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ExtractSearchTokens 提取有效关键词 Token 列表
// 例如："庆余年 2 4k" -> ["庆余年", "2", "4k"]
func ExtractSearchTokens(keyword string) []string {
	normalized := NormalizeSearchText(keyword)
	if normalized == "" {
		return nil
	}
	parts := strings.Fields(normalized)
	tokens := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			tokens = append(tokens, p)
		}
	}
	return tokens
}

// NormalizeSearchSortField 将搜索排序参数收敛为白名单：
// ""（相关度）、hits、latest、year、score。
func NormalizeSearchSortField(sortField string) string {
	switch strings.ToLower(strings.TrimSpace(sortField)) {
	case "hits":
		return "hits"
	case "update_stamp", "latest":
		return "latest"
	case "year":
		return "year"
	case "score", "rating":
		return "score"
	default:
		return ""
	}
}

// TokenMatchesText 判定单 Token 是否命中目标文本（支持数字/罗马数字别名映射）
func TokenMatchesText(token, text string) bool {
	if token == "" || text == "" {
		return false
	}
	if strings.Contains(text, token) {
		return true
	}
	if aliases, ok := numberAliases[token]; ok {
		for _, alias := range aliases {
			if strings.Contains(text, alias) {
				return true
			}
		}
	}
	return false
}

// IsSubsequence 判定 pattern 的所有字符是否按顺序出现在 text 中
// 例如：IsSubsequence("凡人传", "凡人修仙传") == true
func IsSubsequence(pattern, text string) bool {
	pRunes := []rune(pattern)
	tRunes := []rune(text)
	if len(pRunes) == 0 {
		return true
	}
	if len(pRunes) > len(tRunes) {
		return false
	}
	pIdx := 0
	for _, r := range tRunes {
		if r == pRunes[pIdx] {
			pIdx++
			if pIdx == len(pRunes) {
				return true
			}
		}
	}
	return false
}

// IsAsciiAlphaNum 判断是否全为 ASCII 字母或数字
func IsAsciiAlphaNum(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// FilmSearchItem 搜索索引项
type FilmSearchItem struct {
	Mid               int64
	Name              string
	CleanName         string
	PinyinFull        string
	PinyinSyllables   []string
	PinyinInitials    string
	PinyinInitialAlts string
	SubTitle          string
	Actor             string
	Director          string
	AliasSegs         []string
	AliasWords        []string
	PersonSegs        []string
	PersonWords       []string
	Hits              int64
	Score             float64
	Year              int64
	UpdateStamp       int64
}

// FillSearchDerivedFields 建索引时预计算清洗文本、拼音、别名/人名片段，查询热路径不再切分或 OpenCC。
func FillSearchDerivedFields(item *FilmSearchItem) {
	if item == nil {
		return
	}
	normName := TraditionalToSimplified(item.Name)
	item.CleanName = cleanCompactNormalized(normName)
	item.PinyinFull = pinyinFullNormalized(normName)
	item.PinyinSyllables = pinyinSyllablesNormalized(normName)
	variants := pinyinInitialVariantsNormalized(normName)
	if len(variants) > 0 {
		item.PinyinInitials = variants[0]
		if len(variants) > 1 {
			item.PinyinInitialAlts = strings.Join(variants[1:], " ")
		}
	}
	item.AliasSegs, item.AliasWords = BuildAliasFields(item.SubTitle)
	item.PersonSegs, item.PersonWords = BuildPersonFields(item.Actor, item.Director)
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
	if len(item.PinyinSyllables) > 0 {
		if s, ok := matchPinyinSyllables(item.PinyinSyllables, cleanAsciiKey); ok {
			best = raiseScore(best, s)
		}
	} else if item.PinyinFull != "" {
		if len(cleanAsciiKey) >= 3 && item.PinyinFull == cleanAsciiKey {
			best = raiseScore(best, 400)
		} else if len(cleanAsciiKey) >= 4 && strings.HasPrefix(item.PinyinFull, cleanAsciiKey) {
			diff := len(item.PinyinFull) - len(cleanAsciiKey)
			score := 360 - diff*3
			if score < 310 {
				score = 310
			}
			best = raiseScore(best, score)
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

func isFieldSegmentSplitter(r rune) bool {
	switch r {
	case '/', '|', ',', '，', '、', ';', '；', '\n', '\t':
		return true
	default:
		return false
	}
}

func splitFieldSegments(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, isFieldSegmentSplitter)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func looksLikeRelatedTitleDump(raw string) bool {
	return len(splitFieldSegments(raw)) >= 3
}

func extractAsciiWords(raw string) []string {
	if raw == "" {
		return nil
	}
	lower := strings.ToLower(raw)
	return strings.FieldsFunc(lower, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
}

func BuildAliasFields(raw string) (segs []string, words []string) {
	words = extractAsciiWords(raw)
	if looksLikeRelatedTitleDump(raw) {
		return nil, words
	}
	parts := splitFieldSegments(raw)
	if len(parts) == 0 {
		c := CleanCompactText(raw)
		if c == "" {
			return nil, words
		}
		if len([]rune(c)) > 24 {
			return nil, words
		}
		return []string{c}, words
	}
	segs = make([]string, 0, len(parts))
	for _, p := range parts {
		c := CleanCompactText(p)
		if c != "" {
			segs = append(segs, c)
		}
	}
	return segs, words
}

func BuildPersonFields(actor, director string) (segs []string, words []string) {
	appendPersons := func(raw string) {
		if strings.TrimSpace(raw) == "" {
			return
		}
		words = append(words, extractAsciiWords(raw)...)
		parts := splitFieldSegments(raw)
		if len(parts) == 0 {
			parts = []string{raw}
		}
		for _, p := range parts {
			c := CleanCompactText(p)
			if c == "" {
				continue
			}
			if len([]rune(c)) > 16 {
				continue
			}
			segs = append(segs, c)
		}
	}
	appendPersons(actor)
	appendPersons(director)
	return segs, words
}

func scoreAsciiWordList(words []string, q QueryContext, exactScore, containsScore int) int {
	if len(words) == 0 || q.CleanRunes < 3 || len(q.Tokens) > 1 {
		return 0
	}
	target := strings.ToLower(q.CleanKey)
	best := 0
	for _, w := range words {
		if w == target {
			return exactScore
		}
		if len(target) >= 4 && strings.HasPrefix(w, target) {
			best = raiseScore(best, containsScore)
		}
	}
	return best
}

func scorePersonSegs(segs, words []string, q QueryContext) int {
	if q.IsAsciiOnly {
		return scoreAsciiWordList(words, q, 210, 190)
	}
	if q.CleanRunes < 2 || q.CleanRunes >= 5 || len(segs) == 0 {
		return 0
	}
	best := 0
	for _, cSeg := range segs {
		if cSeg == q.CleanKey {
			best = raiseScore(best, 210)
			continue
		}
		if strings.Contains(cSeg, q.CleanKey) {
			best = raiseScore(best, 190)
		}
	}
	return best
}

func scoreAliasSegs(segs, words []string, q QueryContext) int {
	if q.IsAsciiOnly {
		best := scoreAsciiWordList(words, q, 290, 260)
		if q.CleanRunes < 3 {
			return best
		}
		for i, c := range segs {
			if c == q.CleanKey {
				if i == 0 || q.CleanRunes < 4 {
					best = raiseScore(best, 290)
				}
				continue
			}
			if strings.HasPrefix(c, q.CleanKey) {
				best = raiseScore(best, 260)
				continue
			}
			if strings.Contains(c, q.CleanKey) && len([]rune(c)) <= q.CleanRunes+4 {
				best = raiseScore(best, 260)
			}
		}
		return best
	}
	if len(segs) == 0 {
		return 0
	}
	best := 0
	for i, c := range segs {
		if c == q.CleanKey {
			if i == 0 || q.CleanRunes < 4 {
				best = raiseScore(best, 290)
			}
			continue
		}
		if strings.HasPrefix(c, q.CleanKey) {
			best = raiseScore(best, 260)
			continue
		}
		if strings.Contains(c, q.CleanKey) && len([]rune(c)) <= q.CleanRunes+4 {
			best = raiseScore(best, 260)
		}
	}
	return best
}

func preparedSecondary(item *FilmSearchItem) {
	if len(item.AliasSegs) == 0 && len(item.AliasWords) == 0 && item.SubTitle != "" {
		item.AliasSegs, item.AliasWords = BuildAliasFields(item.SubTitle)
	}
	if len(item.PersonSegs) == 0 && len(item.PersonWords) == 0 && (item.Actor != "" || item.Director != "") {
		item.PersonSegs, item.PersonWords = BuildPersonFields(item.Actor, item.Director)
	}
}

func tokenHitsPersonOrAlias(token string, item FilmSearchItem) bool {
	cTok := token
	if cTok == "" {
		return false
	}
	if IsAsciiAlphaNum(cTok) {
		if len(cTok) < 2 {
			return false
		}
		allowPrefix := len(cTok) >= 4
		for _, w := range item.AliasWords {
			if w == cTok || (allowPrefix && strings.HasPrefix(w, cTok)) {
				return true
			}
		}
		for _, w := range item.PersonWords {
			if w == cTok || (allowPrefix && strings.HasPrefix(w, cTok)) {
				return true
			}
		}
		return false
	}
	n := len([]rune(cTok))
	if n >= 2 && n < 5 {
		for _, seg := range item.PersonSegs {
			if seg == cTok || strings.Contains(seg, cTok) {
				return true
			}
		}
	}
	for i, c := range item.AliasSegs {
		if c == cTok {
			if i == 0 || n < 4 {
				return true
			}
			continue
		}
		if strings.HasPrefix(c, cTok) {
			return true
		}
		if strings.Contains(c, cTok) && len([]rune(c)) <= n+4 {
			return true
		}
	}
	return false
}

// ScoreFilmMatch 计算影视与搜索词的匹配度得分（0 表示不匹配）
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

	if cleanName == q.CleanKey {
		return 1000
	}

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

	if len(q.CleanTokens) > 1 {
		preparedSecondary(&item)
		nameHits := 0
		crossHits := 0
		tokenLen := 0
		for _, tok := range q.CleanTokens {
			tokenLen += len([]rune(tok))
			if TokenMatchesText(tok, cleanName) {
				nameHits++
				crossHits++
				continue
			}
			if tokenHitsPersonOrAlias(tok, item) {
				crossHits++
			}
		}
		if nameHits == len(q.CleanTokens) {
			score := 600 - (nameRunes-tokenLen)*6
			if score < 520 {
				score = 520
			}
			bestScore = raiseScore(bestScore, score)
		} else if crossHits == len(q.CleanTokens) {
			bestScore = raiseScore(bestScore, 450)
		}
	}

	if !q.IsAsciiOnly && queryRunes >= 3 && bestScore < 620 && IsSubsequence(q.CleanKey, cleanName) {
		score := 480 - (nameRunes-queryRunes)*6
		if score < 400 {
			score = 400
		}
		bestScore = raiseScore(bestScore, score)
	}

	if q.IsAsciiOnly {
		bestScore = raiseScore(bestScore, scorePinyinMatch(item, strings.ToLower(q.CleanKey)))
	}

	// 片名已经高相关时不再扫主演/副标题
	if bestScore >= 620 {
		return bestScore
	}

	preparedSecondary(&item)
	bestScore = raiseScore(bestScore, scoreAliasSegs(item.AliasSegs, item.AliasWords, q))
	bestScore = raiseScore(bestScore, scorePersonSegs(item.PersonSegs, item.PersonWords, q))
	return bestScore
}
