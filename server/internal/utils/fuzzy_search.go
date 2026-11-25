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
	if s == "" || compactIsIdentity(s) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if out, ok := compactRune(r); ok {
			b.WriteRune(out)
		}
	}
	return b.String()
}

func compactIsIdentity(s string) bool {
	for _, r := range s {
		out, ok := compactRune(r)
		if !ok || out != r {
			return false
		}
	}
	return true
}

func compactRune(r rune) (rune, bool) {
	if r >= 0xFF01 && r <= 0xFF5E {
		r = r - 0xFEE0
	}
	if r < 128 {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r, true
		}
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A'), true
		}
		return 0, false
	}
	if unicode.Is(unicode.Han, r) {
		return r, true
	}
	return 0, false
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

// FilmSearchItem 搜索索引项（纯片名与拼音轻量结构，零常驻 slice）
