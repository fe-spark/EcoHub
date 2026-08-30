package utils

import (
	"strings"
	"unicode"

	"github.com/mozillazg/go-pinyin"
)

const maxPinyinInitialVariants = 8

var (
	pinyinArgsSingle = pinyin.NewArgs()
	pinyinArgsHetero = pinyin.NewArgs()
	customCharPinyin = map[rune][]string{
		'传': {"chuan", "zhuan"},
		'重': {"chong", "zhong"},
		'行': {"xing", "hang"},
		'长': {"chang", "zhang"},
		'乐': {"le", "yue"},
		'差': {"cha", "chai"},
		'降': {"jiang", "xiang"},
		'藏': {"cang", "zang"},
	}
)

func init() {
	pinyinArgsSingle.Style = pinyin.Normal
	pinyinArgsSingle.Fallback = func(r rune, args pinyin.Args) []string {
		return []string{string(r)}
	}

	pinyinArgsHetero.Style = pinyin.Normal
	pinyinArgsHetero.Heteronym = true
	pinyinArgsHetero.Fallback = func(r rune, args pinyin.Args) []string {
		return []string{string(r)}
	}
}

// ToPinyin 将中文字符串转换为无音调全拼（保留英数，去除非英数符号并转小写）
// 例如："流浪地球 2" -> "liulangdiqiu2"
func ToPinyin(s string) string {
	return pinyinFullNormalized(TraditionalToSimplified(s))
}

func pinyinFullNormalized(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		if r < 128 {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
			} else if r >= 'A' && r <= 'Z' {
				b.WriteRune(r + ('a' - 'A'))
			}
			continue
		}
		if unicode.Is(unicode.Han, r) {
			if custom, ok := customCharPinyin[r]; ok && len(custom) > 0 {
				b.WriteString(custom[0])
				continue
			}
			pys := pinyin.SinglePinyin(r, pinyinArgsSingle)
			if len(pys) > 0 && len(pys[0]) > 0 {
				b.WriteString(strings.ToLower(pys[0]))
			}
		}
	}
	return b.String()
}

// ToPinyinSyllables 将中文字符串转换为单个音节列表（保留英数小写）
// 例如："流浪地球 2" -> ["liu", "lang", "di", "qiu", "2"]，"小夜测试" -> ["xiao", "ye", "ce", "shi"]
func ToPinyinSyllables(s string) []string {
	return pinyinSyllablesNormalized(TraditionalToSimplified(s))
}

func pinyinSyllablesNormalized(s string) []string {
	if s == "" {
		return nil
	}
	var syllables []string
	var asciiBuf strings.Builder

	flushAscii := func() {
		if asciiBuf.Len() > 0 {
			syllables = append(syllables, strings.ToLower(asciiBuf.String()))
			asciiBuf.Reset()
		}
	}

	for _, r := range s {
		if r < 128 {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				asciiBuf.WriteRune(r)
			} else if r >= 'A' && r <= 'Z' {
				asciiBuf.WriteRune(r + ('a' - 'A'))
			} else {
				flushAscii()
			}
			continue
		}
		flushAscii()
		if unicode.Is(unicode.Han, r) {
			if custom, ok := customCharPinyin[r]; ok && len(custom) > 0 {
				syllables = append(syllables, custom[0])
				continue
			}
			pys := pinyin.SinglePinyin(r, pinyinArgsSingle)
			if len(pys) > 0 && len(pys[0]) > 0 {
				syllables = append(syllables, strings.ToLower(pys[0]))
			}
		}
	}
	flushAscii()
	return syllables
}

// ToPinyinInitials 将中文字符串转换为首字母拼音简拼（保留英数）
// 例如："流浪地球 2" -> "lldq2"，"庆余年" -> "qyn"
func ToPinyinInitials(s string) string {
	variants := ToPinyinInitialVariants(s)
	if len(variants) == 0 {
		return ""
	}
	return variants[0]
}

// ToPinyinInitialVariants 预计算简拼及其多音字变体，供索引阶段写入，查询热路径不再现场转拼音。
// 第一个元素与 ToPinyinInitials 一致。
func ToPinyinInitialVariants(s string) []string {
	return pinyinInitialVariantsNormalized(TraditionalToSimplified(s))
}

func pinyinInitialVariantsNormalized(s string) []string {
	if s == "" {
		return nil
	}

	sets := make([][]byte, 0, len(s))
	for _, r := range s {
		if r < 128 {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				sets = append(sets, []byte{byte(r)})
			} else if r >= 'A' && r <= 'Z' {
				sets = append(sets, []byte{byte(r + ('a' - 'A'))})
			}
			continue
		}
		if unicode.Is(unicode.Han, r) {
			if opts := hanInitialOptions(r); len(opts) > 0 {
				sets = append(sets, opts)
			}
		}
	}
	return expandInitialVariants(sets)
}

func hanInitialOptions(r rune) []byte {
	seen := make(map[byte]struct{}, 4)
	set := make([]byte, 0, 4)
	add := func(c byte) {
		if c == 0 {
			return
		}
		if _, ok := seen[c]; ok {
			return
		}
		seen[c] = struct{}{}
		set = append(set, c)
	}

	if custom, ok := customCharPinyin[r]; ok {
		for _, py := range custom {
			if len(py) > 0 {
				add(py[0])
			}
		}
		return set
	}

	pys := pinyin.SinglePinyin(r, pinyinArgsSingle)
	if len(pys) > 0 && len(pys[0]) > 0 {
		add(strings.ToLower(pys[0])[0])
	}
	het := pinyin.SinglePinyin(r, pinyinArgsHetero)
	for _, py := range het {
		if len(py) > 0 {
			add(strings.ToLower(py)[0])
		}
	}
	return set
}

func expandInitialVariants(sets [][]byte) []string {
	if len(sets) == 0 {
		return nil
	}
	variants := []string{""}
	for _, opts := range sets {
		if len(opts) == 0 {
			continue
		}
		next := make([]string, 0, min(len(variants)*len(opts), maxPinyinInitialVariants))
		for _, prefix := range variants {
			for _, o := range opts {
				next = append(next, prefix+string(o))
				if len(next) >= maxPinyinInitialVariants {
					break
				}
			}
			if len(next) >= maxPinyinInitialVariants {
				break
			}
		}
		variants = next
	}

	out := make([]string, 0, len(variants))
	seen := make(map[string]struct{}, len(variants))
	for _, v := range variants {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// MatchPinyinInitials 判定 query（纯英数）是否等于或包含于 target 的简拼变体。
// 仅用于单测与离线校验；查询热路径请使用预计算的 PinyinInitials / PinyinInitialAlts。
func MatchPinyinInitials(query string, targetText string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" || targetText == "" {
		return false
	}
	for _, v := range ToPinyinInitialVariants(targetText) {
		if v == query || strings.Contains(v, query) {
			return true
		}
	}
	return false
}
