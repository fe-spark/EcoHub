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
	s = TraditionalToSimplified(s)
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
	CleanName         string // 紧凑纯文本
	PinyinFull        string // 全拼
	PinyinSyllables   string // 空格分隔的拼音音节，例如 "xiao ye ce shi"
	PinyinInitials    string // 简拼（首选读音）
	PinyinInitialAlts string // 多音字简拼变体，空格分隔
	SubTitle          string
	CleanSubTitle     string
	Actor             string
	CleanActor        string
	Director          string
	CleanDirector     string
	Hits              int64
	Score             float64
	Year              int64
	UpdateStamp       int64
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
	n := len(syllables)
	for i := 0; i < n; i++ {
		var combined strings.Builder
		for j := i; j < n; j++ {
			combined.WriteString(syllables[j])
			cStr := combined.String()
			if cStr == cleanAsciiKey {
				if i == 0 && j == n-1 {
					return 430, true // 全名拼音完全匹配，如 "ceshi" 命中 "测试"
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
			if len(cStr) > len(cleanAsciiKey) {
				// 仅当至少匹配了 1 个完整首音节，且后续音节以前缀形式匹配时（如 "liul" 匹配 "liu" + "lang..."）
				if i == 0 && j > 0 && strings.HasPrefix(cStr, cleanAsciiKey) {
					s := 360 - (len(cStr)-len(cleanAsciiKey))*3
					if s < 310 {
						s = 310
					}
					return s, true
				}
				break
			}
		}
	}
	return 0, false
}

func scorePinyinMatch(item FilmSearchItem, cleanAsciiKey string) int {
	best := 0
	// 1. 优先基于独立拼音音节序列匹配（精准避免 cross-syllable 误伤，如 xian 与 xiang）
	if item.PinyinSyllables != "" {
		syllables := strings.Fields(item.PinyinSyllables)
		if s, ok := matchPinyinSyllables(syllables, cleanAsciiKey); ok {
			best = raiseScore(best, s)
		}
	} else if item.PinyinFull != "" {
		// 降级兜底（未生成音节时）
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

	// 2. 简拼首字母匹配（如 "lldq", "qyn"）
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

func matchAsciiWords(raw string, q QueryContext, exactScore, containsScore int) int {
	if raw == "" || q.CleanRunes < 3 {
		return 0
	}
	lowerRaw := strings.ToLower(raw)
	if len(q.Tokens) > 1 {
		if strings.Contains(lowerRaw, strings.ToLower(q.RawKey)) {
			return containsScore
		}
		return 0
	}
	words := strings.FieldsFunc(lowerRaw, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	target := strings.ToLower(q.CleanKey)
	for _, w := range words {
		if w == target {
			return exactScore
		}
		if len(target) >= 4 && strings.HasPrefix(w, target) {
			return containsScore
		}
	}
	return 0
}

// matchPersonField 主演/导演：按人名片段匹配，禁止把采集源的相关作品墙当演员表。
// 5 个及以上汉字更像片名，不走此字段。
func matchPersonField(raw string, q QueryContext, exactScore, containsScore int) int {
	if strings.TrimSpace(raw) == "" {
		return 0
	}
	if q.IsAsciiOnly {
		return matchAsciiWords(raw, q, exactScore, containsScore)
	}
	if q.CleanRunes < 2 || q.CleanRunes >= 5 {
		return 0
	}
	segs := splitFieldSegments(raw)
	if len(segs) == 0 {
		segs = []string{raw}
	}
	best := 0
	for _, seg := range segs {
		cSeg := CleanCompactText(seg)
		if cSeg == "" {
			continue
		}
		segRunes := len([]rune(cSeg))
		if segRunes > 16 {
			continue
		}
		if cSeg == q.CleanKey {
			best = raiseScore(best, exactScore)
			continue
		}
		if strings.Contains(cSeg, q.CleanKey) {
			best = raiseScore(best, containsScore)
		}
	}
	return best
}

// matchAliasField 副标题/别名：只把短字段或 1～2 段当别名，3 段以上视为相关作品墙。
func matchAliasField(raw string, q QueryContext, exactScore, containsScore int) int {
	if strings.TrimSpace(raw) == "" {
		return 0
	}
	if looksLikeRelatedTitleDump(raw) {
		return 0
	}
	if q.IsAsciiOnly {
		return matchAsciiWords(raw, q, exactScore, containsScore)
	}
	segs := splitFieldSegments(raw)
	if len(segs) == 0 {
		segs = []string{raw}
	}
	best := 0
	for i, seg := range segs {
		c := CleanCompactText(seg)
		if c == "" {
			continue
		}
		if c == q.CleanKey {
			// 4 字以上片名只认第一段别名，避免「斗罗大陆 / 凡人修仙传」这种相关列表
			if i == 0 || q.CleanRunes < 4 {
				best = raiseScore(best, exactScore)
			}
			continue
		}
		if strings.HasPrefix(c, q.CleanKey) {
			best = raiseScore(best, containsScore)
			continue
		}
		// 允许极短后缀（第一季、2），禁止把多部片名拼在一段里 contains
		if strings.Contains(c, q.CleanKey) && len([]rune(c)) <= q.CleanRunes+4 {
			best = raiseScore(best, containsScore)
		}
	}
	return best
}

func tokenHitsPersonOrAlias(token string, item FilmSearchItem) bool {
	cTok := CleanCompactText(token)
	if cTok == "" {
		return false
	}
	q := QueryContext{
		RawKey:      token,
		CleanKey:    cTok,
		Tokens:      []string{token},
		CleanTokens: []string{cTok},
		CleanRunes:  len([]rune(cTok)),
		IsAsciiOnly: IsAsciiAlphaNum(cTok),
	}
	return matchAliasField(item.SubTitle, q, 1, 1) > 0 ||
		matchPersonField(item.Actor, q, 1, 1) > 0 ||
		matchPersonField(item.Director, q, 1, 1) > 0
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

	// 1. 片名完全匹配 (1000 分)
	if cleanName == q.CleanKey {
		return 1000
	}

	// 2. 片名前缀匹配 (850 ~ 750 分)
	if strings.HasPrefix(cleanName, q.CleanKey) {
		score := 850 - (nameRunes-queryRunes)*8
		if score < 750 {
			score = 750
		}
		bestScore = raiseScore(bestScore, score)
	} else if strings.Contains(cleanName, q.CleanKey) {
		// 3. 片名连续子串包含匹配 (720 ~ 620 分)
		score := 720 - (nameRunes-queryRunes)*8
		if score < 620 {
			score = 620
		}
		bestScore = raiseScore(bestScore, score)
	}

	// 4. 多词 Token：同字段（片名）命中优先，跨字段 AND 次之
	if len(q.CleanTokens) > 1 {
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

	// 5. 片名子序列跳字匹配 (480 ~ 400 分)：仅对中文词生效（>=3 字），避免拼音字母与短词误伤
	if !q.IsAsciiOnly && queryRunes >= 3 && IsSubsequence(q.CleanKey, cleanName) {
		score := 480 - (nameRunes-queryRunes)*6
		if score < 400 {
			score = 400
		}
		bestScore = raiseScore(bestScore, score)
	}

	// 6. 拼音匹配：只用索引预计算字段与拼音音节
	if q.IsAsciiOnly {
		bestScore = raiseScore(bestScore, scorePinyinMatch(item, strings.ToLower(q.CleanKey)))
	}

	// 7. 副标题 / 别名 / 原名匹配 (290 ~ 260 分)
	bestScore = raiseScore(bestScore, matchAliasField(item.SubTitle, q, 290, 260))

	// 8. 主演匹配 (210 ~ 190 分)
	bestScore = raiseScore(bestScore, matchPersonField(item.Actor, q, 210, 190))

	// 9. 导演匹配 (190 ~ 170 分)
	bestScore = raiseScore(bestScore, matchPersonField(item.Director, q, 190, 170))

	return bestScore
}
