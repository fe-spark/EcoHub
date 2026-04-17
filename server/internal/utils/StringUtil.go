package utils

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"
)

var seriesSuffixPatterns = []*regexp.Regexp{
	regexp.MustCompile(`[\s·\-_]*第?\s*[一二三四五六七八九十百千万零两0-9]+\s*(季|部|篇|章)$`),
	regexp.MustCompile(`(?i)[\s·\-_]*(season\s*[0-9]+|s[0-9]{1,2})$`),
	regexp.MustCompile(`(?:\(|（|\[|【)\s*第?\s*[一二三四五六七八九十百千万零两0-9]+\s*(季|部|篇|章)\s*(?:\)|）|\]|】)$`),
}

var (
	titleSpacePattern      = regexp.MustCompile(`\s+`)
	segmentBoundaryPattern = `[\s·\-_/：:．。、,，]*`
	segmentNumeralPattern  = `([一二三四五六七八九十百千万零两〇0-9]+)`
	segmentUnitPattern     = `(季|期|部|篇|章|卷|弹|话|回|幕|集)`
	segmentSuffixPatterns  = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^(.+?)` + segmentBoundaryPattern + `第?` + segmentNumeralPattern + segmentUnitPattern + `$`),
		regexp.MustCompile(`(?i)^(.+?)` + segmentBoundaryPattern + `(?:season|series|part|vol|volume)\s*([0-9]+)` + `$`),
		regexp.MustCompile(`(?i)^(.+?)` + segmentBoundaryPattern + `s\s*([0-9]{1,2})` + `$`),
		regexp.MustCompile(`(?i)^(.+?)` + segmentBoundaryPattern + `(?:ep|episode)\s*([0-9]{1,3})` + `$`),
	}
	segmentBracketPattern = regexp.MustCompile(`(?i)[\s·\-_/：:．。、,，]*(?:\(|（|\[|【)\s*(第?` + segmentNumeralPattern + segmentUnitPattern + `|(?:season|series|part|vol|volume)\s*[0-9]+|s\s*[0-9]{1,2}|(?:ep|episode)\s*[0-9]{1,3})\s*(?:\)|）|\]|】)\s*$`)
	baseTrimPattern       = regexp.MustCompile(`^[\s·\-_/：:．。、,，]+|[\s·\-_/：:．。、,，]+$`)
	seriesNoisePatterns   = []*regexp.Regexp{
		regexp.MustCompile(`(?i)[\s·\-_/：:．。、,，]*(?:\(|（|\[|【)?(?:19|20)\d{2}(?:\)|）|\]|】)?\s*$`),
		regexp.MustCompile(`(?i)[\s·\-_/：:．。、,，]*(?:\(|（|\[|【)?(?:4k|8k|2160p|1080p|720p|480p|bd|bdrip|blu\s*ray|bluray|web\s*-?dl|webrip|hdtv|hd|uhd)(?:\)|）|\]|】)?\s*$`),
		regexp.MustCompile(`(?i)[\s·\-_/：:．。、,，]*(?:\(|（|\[|【)?(?:国语|国粤双语|粤语|英语|日语|韩语|中字|中英字幕|双字|双语|english|japanese|korean|mandarin|cantonese|dubbed|subbed)(?:\)|）|\]|】)?\s*$`),
		regexp.MustCompile(`(?i)[\s·\-_/：:．。、,，]*(?:\(|（|\[|【)?(?:完整版|未删减版|未删减|加长版|特别篇|特别版|剧场版|tv版|tv动画|ova|oad|sp|总集篇|合集|修复版|重制版|纪念版)(?:\)|）|\]|】)?\s*$`),
		regexp.MustCompile(`(?i)[\s·\-_/：:．。、,，]*(?:\(|（|\[|【)?(?:更新至?.*|全\d+集|完结|已完结|连载中|上映版|抢先版|超清版)(?:\)|）|\]|】)?\s*$`),
	}
	seriesAliasSplitPattern = regexp.MustCompile(`\s*(?:/|\||,|，|、|:|：)\s*`)
)

var segmentUnitAlias = map[string]string{
	"季": "season",
	"期": "issue",
	"部": "part",
	"篇": "arc",
	"章": "chapter",
	"卷": "volume",
	"弹": "shot",
	"话": "episode",
	"回": "round",
	"幕": "act",
	"集": "collection",
}

var englishSegmentTypeAlias = map[string]string{
	"season":  "season",
	"series":  "season",
	"part":    "part",
	"vol":     "volume",
	"volume":  "volume",
	"ep":      "episode",
	"episode": "episode",
}

// GenerateUUID 生成UUID
func GenerateUUID() (uuid string) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		log.Fatal(err)
	}
	uuid = fmt.Sprintf("%X-%X-%X-%X-%X",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
	return
}

// RandomString 生成指定长度两倍的随机字符串
func RandomString(length int) (uuid string) {
	b := make([]byte, length)
	_, err := rand.Read(b)
	if err != nil {
		log.Fatal(err)
	}
	uuid = fmt.Sprintf("%x", b)
	return
}

// GenerateSalt 生成 length为16的随机字符串
func GenerateSalt() (uuid string) {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		log.Fatal(err)
	}
	uuid = fmt.Sprintf("%X", b)
	return
}

// PasswordEncrypt 密码加密 , (password+salt) md5 * 3
func PasswordEncrypt(password, salt string) string {
	b := fmt.Append(nil, password, salt) // 将字符串转换为字节切片
	var r [16]byte
	for range 3 {
		r = md5.Sum(b) // 调用md5.Sum()函数进行加密
		b = []byte(hex.EncodeToString(r[:]))
	}
	return hex.EncodeToString(r[:])
}

// ValidURL 校验http链接是否是符合规范的URL
func ValidURL(s string) bool {
	_, err := url.ParseRequestURI(s)
	return err == nil
}

func ValidPwd(s string) error {
	if len(s) < 8 || len(s) > 12 {
		return fmt.Errorf("密码长度不符合规范, 必须为8-10位")
	}
	// 分别校验数字 大小写字母和特殊字符
	num := `[0-9]{1}`
	l := `[a-z]{1}`
	u := `[A-Z]{1}`
	symbol := `[!@#~$%^&*()+|_]{1}`
	if b, err := regexp.MatchString(num, s); !b || err != nil {
		return errors.New("密码必须包含数字 ")
	}
	if b, err := regexp.MatchString(l, s); !b || err != nil {
		return errors.New("密码必须包含小写字母")
	}
	if b, err := regexp.MatchString(u, s); !b || err != nil {
		return errors.New("密码必须包含大写字母")
	}
	if b, err := regexp.MatchString(symbol, s); !b || err != nil {
		return errors.New("密码必须包含特殊字")
	}
	return nil
}

// ContainsAny 判断字符串是否包含切片中的任意一个关键词
func ContainsAny(s string, keywords []string) bool {
	if keywords == nil {
		return false
	}
	for _, kw := range keywords {
		if kw != "" && (s == kw || (len(kw) > 0 && (strings.Contains(s, kw)))) {
			return true
		}
	}
	return false
}

func NormalizeTitleCandidates(title string) []string {
	base := strings.TrimSpace(title)
	if base == "" {
		return nil
	}

	candidates := make([]string, 0, 6)
	seen := make(map[string]struct{}, 6)
	appendCandidate := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		candidates = append(candidates, v)
	}

	appendCandidate(base)
	compact := regexp.MustCompile(`\s+`).ReplaceAllString(base, "")
	appendCandidate(compact)

	for _, p := range seriesSuffixPatterns {
		trimmed := strings.TrimSpace(p.ReplaceAllString(base, ""))
		appendCandidate(trimmed)
		if trimmed != "" {
			appendCandidate(regexp.MustCompile(`\s+`).ReplaceAllString(trimmed, ""))
		}
	}

	return candidates
}

func NormalizeCollectionTitle(title string) string {
	base, segment := splitTrailingSegment(title)
	base = normalizeCollectionBaseTitle(base)
	if base == "" {
		return ""
	}
	if segment == "" {
		return base
	}
	return base + "#" + segment
}

func BuildCollectionDbIdentity(dbID int64, title string) string {
	if dbID == 0 {
		return ""
	}
	_, segment := splitTrailingSegment(title)
	if segment == "" {
		return fmt.Sprintf("dbid_%d", dbID)
	}
	return fmt.Sprintf("dbid_%d#%s", dbID, segment)
}

func BuildSeriesKey(title string, subTitle string) string {
	base := normalizeSeriesTitleCandidate(title)
	if base == "" {
		for _, alias := range splitSeriesAliasCandidates(subTitle) {
			base = normalizeSeriesTitleCandidate(alias)
			if base != "" {
				break
			}
		}
	}
	if base == "" {
		return ""
	}
	return "series_" + GenerateHashKey(base)
}

func normalizeCollectionBaseTitle(title string) string {
	title = TraditionalToSimplified(strings.TrimSpace(title))
	title = titleSpacePattern.ReplaceAllString(title, " ")
	title = baseTrimPattern.ReplaceAllString(title, "")
	return strings.TrimSpace(title)
}

func normalizeSeriesTitleCandidate(title string) string {
	title, _ = splitTrailingSegment(title)
	title = stripSeriesNoiseSuffix(title)
	title = normalizeCollectionBaseTitle(title)
	if title == "" {
		return ""
	}
	return strings.ToLower(title)
}

func stripSeriesNoiseSuffix(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	for {
		trimmed := title
		for _, pattern := range seriesNoisePatterns {
			trimmed = strings.TrimSpace(pattern.ReplaceAllString(trimmed, ""))
		}
		trimmed = baseTrimPattern.ReplaceAllString(trimmed, "")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed == title {
			return trimmed
		}
		title = trimmed
	}
}

func splitSeriesAliasCandidates(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := seriesAliasSplitPattern.Split(raw, -1)
	aliases := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		aliases = append(aliases, part)
	}
	return aliases
}

func splitTrailingSegment(title string) (string, string) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", ""
	}

	if loc := segmentBracketPattern.FindStringIndex(title); len(loc) == 2 && loc[1] == len(title) {
		matched := title[loc[0]:loc[1]]
		inner := strings.TrimSpace(matched)
		inner = strings.TrimLeft(inner, " ·-_/：:．。、,，([（【")
		inner = strings.TrimRight(inner, ")]）】")
		inner = strings.TrimSpace(inner)
		if segment := normalizeSegmentToken(inner); segment != "" {
			return title[:loc[0]], segment
		}
	}

	for _, pattern := range segmentSuffixPatterns {
		matched := pattern.FindStringSubmatch(title)
		if len(matched) < 3 {
			continue
		}
		base := matched[1]
		segment := normalizeSegmentToken(strings.TrimSpace(title[len(base):]))
		if segment == "" {
			continue
		}
		return base, segment
	}

	return title, ""
}

func normalizeSegmentToken(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, "()（）[]【】")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = TraditionalToSimplified(raw)
	raw = strings.ToLower(raw)
	raw = titleSpacePattern.ReplaceAllString(raw, "")
	raw = strings.TrimLeft(raw, "·-_/：:．。、,，")

	if matched := regexp.MustCompile(`^第?([一二三四五六七八九十百千万零两〇0-9]+)(季|期|部|篇|章|卷|弹|话|回|幕|集)$`).FindStringSubmatch(raw); len(matched) == 3 {
		if no, ok := parseSeriesNumber(matched[1]); ok {
			return buildTypedSegment(segmentUnitAlias[matched[2]], no)
		}
	}
	if matched := regexp.MustCompile(`^(season|series|part|vol|volume)([0-9]+)$`).FindStringSubmatch(raw); len(matched) == 3 {
		return buildTypedSegment(englishSegmentTypeAlias[matched[1]], parsePositiveInt(matched[2]))
	}
	if matched := regexp.MustCompile(`^s([0-9]{1,2})$`).FindStringSubmatch(raw); len(matched) == 2 {
		return buildTypedSegment("season", parsePositiveInt(matched[1]))
	}
	if matched := regexp.MustCompile(`^(ep|episode)([0-9]{1,3})$`).FindStringSubmatch(raw); len(matched) == 3 {
		return buildTypedSegment(englishSegmentTypeAlias[matched[1]], parsePositiveInt(matched[2]))
	}
	return ""
}

func buildTypedSegment(segmentType string, no int) string {
	if segmentType == "" || no <= 0 {
		return ""
	}
	return segmentType + fmt.Sprint(no)
}

func parsePositiveInt(raw string) int {
	raw = strings.TrimLeft(raw, "0")
	if raw == "" {
		return 0
	}
	n := 0
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

func parseSeriesNumber(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if regexp.MustCompile(`^[0-9]+$`).MatchString(raw) {
		n := 0
		for _, ch := range raw {
			n = n*10 + int(ch-'0')
		}
		if n > 0 {
			return n, true
		}
		return 0, false
	}

	values := map[rune]int{'零': 0, '〇': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	units := map[rune]int{'十': 10, '百': 100, '千': 1000, '万': 10000}
	total := 0
	section := 0
	number := 0
	for _, ch := range []rune(raw) {
		if v, ok := values[ch]; ok {
			number = v
			continue
		}
		unit, ok := units[ch]
		if !ok {
			return 0, false
		}
		if unit == 10000 {
			if number == 0 && section == 0 {
				section = 1
			} else {
				section += number
			}
			total += section * unit
			section = 0
			number = 0
			continue
		}
		if number == 0 {
			number = 1
		}
		section += number * unit
		number = 0
	}
	total += section + number
	if total <= 0 {
		return 0, false
	}
	return total, true
}
