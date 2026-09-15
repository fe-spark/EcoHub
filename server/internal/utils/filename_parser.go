package utils

import (
	"errors"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var (
	ErrNonVideoFile = errors.New("不是支持的媒体视频文件")
	ErrParseFailed  = errors.New("解析片名失败：标题为空或仅包含噪音标签")
)

// ParsedMedia 媒体文件名解析结果
type ParsedMedia struct {
	Title    string // 清洗后的片名
	Year     int64  // 发行年份（0 代表未识别出年份）
	Season   int    // 剧集季度（剧集缺省为 1；电影为 0）
	Episode  int    // 集数（电影为 0）
	TmdbID   int64  // 文件名内嵌的 TMDB ID（如 {tmdb-910850}）
	IsTV     bool   // 是否为剧集
	Hint     string // 特殊标记：disc_split | range_disc | unplayable_container 等
	HintFrom string // 片名来源：filename | parent | grandparent
}

var (
	// 支持的视频容器扩展名
	videoExtMap = map[string]bool{
		".mkv":  true,
		".mp4":  true,
		".avi":  true,
		".mov":  true,
		".wmv":  true,
		".flv":  true,
		".f4v":  true,
		".rmvb": true,
		".rm":   true,
		".m4v":  true,
		".ts":   true,
		".m2ts": true,
		".webm": true,
		".iso":  true,
	}

	// 站点/发布组标签：[LowPower-Raws]、【字幕组】、{tmdb-123}
	sitePrefixRegex   = regexp.MustCompile(`^(?:\[[^\]]+\]|【[^】]+】|\{[^\}]+\})\s*`)
	bracketGroupRegex = regexp.MustCompile(`\[[^\]]*\]|【[^】]*】|\{[^}]*\}`)
	tmdbIDRegex       = regexp.MustCompile(`(?i)\{?\s*tmdb[-_ ]?(\d+)\s*\}?`)
	parenSourceRegex  = regexp.MustCompile(`(?i)\(\s*(?:BD|BDRIP|WEB|WEB-?DL|BLURAY|UHD).*`)
	trailingMovieTag  = regexp.MustCompile(`(?i)[\s._-]+movie\s*\(`)

	// 范围集标识：E01-E03, EP01-EP03
	rangeDiscRegex = regexp.MustCompile(`(?i)\b(?:EP?|E)(\d{1,4})\s*-\s*(?:EP?|E)?(\d{1,4})\b`)

	// 分盘标识：CD1, CD2, Disc 1, Part 1
	discSplitRegex = regexp.MustCompile(`(?i)\b(?:CD|DISC|PART)[.\s_-]*([0-9]+)\b`)

	// 季集正则匹配
	sRegex           = regexp.MustCompile(`(?i)\bS(\d{1,2})\b`)
	seRegex          = regexp.MustCompile(`(?i)\bS(\d{1,2})\s*(?:EP?|E)(\d{1,4})\b`)
	epRegex          = regexp.MustCompile(`(?i)\b(?:EP?|E)(\d{1,4})\b`)
	cnEpisodeRegex   = regexp.MustCompile(`第\s*([0-9一二三四五六七八九十百]+)\s*[集话期]`)
	standaloneDigits = regexp.MustCompile(`^(\d{1,4})$`)

	// 目录季匹配
	dirSeasonRegex = regexp.MustCompile(`(?i)(?:Season|S)\s*(\d{1,2})|第\s*([0-9一二三四五六七八九十百]+)\s*季`)

	// 年份匹配：1900-2099
	yearRegex = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)

	// 从第一个清晰度/片源标记截到末尾（含 [1080P]、中英双字、Amazon 等）
	noiseTokensRegex = regexp.MustCompile(`(?i)[\s._(\-\[【]+(?:2160p|1080p|720p|576p|480p|4k|8k|uhd|bluray|blu-ray|bdrip|remux|web-?dl|webrip|hdtv|amazon|netflix|hdr10?\+?|hdr|sdr|dv|dolby\s*vision|atmos|dts(?:-hd)?|truehd|aac|flac|eac3|ddp?5\.1|ac3|x26[45]|h\.?26[45]|hevc|avc|hi10|10bit|8bit|complete|full\.season|repack|proper|unrated|extended|edition|中英双字|简繁(?:英|日)?|内封|外挂|合集).*$`)

	// 纯规格独立噪音
	standaloneNoiseRegex = regexp.MustCompile(`(?i)^(?:2160p|1080p|720p|576p|480p|4k|8k|uhd|bluray|blu-ray|remux|web-?dl|webrip|hdtv|hdr10?\+?|hdr|dv|atmos|dts|truehd|aac|flac|x26[45]|h\.?26[45]|hevc|avc|10bit|8bit)$`)

	// 中文字符间的点：鬼灭之刃.无限列车篇 -> 鬼灭之刃无限列车篇
	hanDotHanRegex = regexp.MustCompile(`([\p{Han}])\s*[._·]\s*([\p{Han}])`)
)

// ParseMediaFilename 解析相对路径得到影视元数据
func ParseMediaFilename(relPath string, mediaType string) (*ParsedMedia, error) {
	relPath = filepath.ToSlash(strings.TrimSpace(relPath))
	if relPath == "" {
		return nil, ErrParseFailed
	}

	parts := strings.Split(relPath, "/")
	filename := parts[len(parts)-1]
	ext := strings.ToLower(filepath.Ext(filename))

	if !videoExtMap[ext] {
		return nil, ErrNonVideoFile
	}

	res := &ParsedMedia{
		IsTV: strings.EqualFold(mediaType, "tv"),
	}

	if ext == ".iso" {
		res.Hint = "unplayable_container"
	}

	baseName := strings.TrimSuffix(filename, filepath.Ext(filename))
	res.TmdbID = extractTmdbID(baseName + " " + relPath)
	for sitePrefixRegex.MatchString(baseName) {
		baseName = strings.TrimSpace(sitePrefixRegex.ReplaceAllString(baseName, ""))
	}

	// 1. 探测分盘与拼接盘标记
	if m := rangeDiscRegex.FindStringSubmatch(baseName); len(m) > 1 {
		res.Hint = "range_disc"
		if ep, err := strconv.Atoi(m[1]); err == nil {
			res.Episode = ep
		}
	} else if discSplitRegex.MatchString(baseName) {
		res.Hint = "disc_split"
	}

	// 2. 解析季与集（仅在剧集模式下生效，或从文件名提取）
	if res.IsTV {
		parseSeasonAndEpisode(baseName, parts, res)
	}

	// 3. 解析年份（优先文件名，次选父目录，再选祖父目录）
	res.Year = extractYear(baseName, parts)

	// 4. 解析片名并清理噪音
	title, hintFrom := extractTitle(baseName, parts, res)
	if title == "" {
		return nil, ErrParseFailed
	}

	res.Title = title
	res.HintFrom = hintFrom

	if res.IsTV && res.Season == 0 {
		res.Season = 1
	}

	return res, nil
}

func parseSeasonAndEpisode(baseName string, parts []string, res *ParsedMedia) {
	// 2.1 优先匹配 S01E02 组合
	if sm := seRegex.FindStringSubmatch(baseName); len(sm) > 2 {
		if s, err := strconv.Atoi(sm[1]); err == nil {
			res.Season = s
		}
		if res.Episode == 0 {
			if e, err := strconv.Atoi(sm[2]); err == nil {
				res.Episode = e
			}
		}
		return
	}

	// 2.2 匹配单集 E01 / EP01 / 第01集 / 01
	if res.Episode == 0 {
		if em := epRegex.FindStringSubmatch(baseName); len(em) > 1 {
			if e, err := strconv.Atoi(em[1]); err == nil {
				res.Episode = e
			}
		} else if cm := cnEpisodeRegex.FindStringSubmatch(baseName); len(cm) > 1 {
			res.Episode = parseNumeral(cm[1])
		} else if dm := standaloneDigits.FindStringSubmatch(baseName); len(dm) > 1 {
			if e, err := strconv.Atoi(dm[1]); err == nil {
				res.Episode = e
			}
		}
	}

	// 2.3 匹配季度 S01
	if res.Season == 0 {
		if sm := sRegex.FindStringSubmatch(baseName); len(sm) > 1 {
			if s, err := strconv.Atoi(sm[1]); err == nil {
				res.Season = s
			}
		}
	}

	// 2.4 文件名未包含季度时，回溯父目录或祖父目录
	if res.Season == 0 && len(parts) > 1 {
		for i := len(parts) - 2; i >= 0; i-- {
			part := parts[i]
			if dm := dirSeasonRegex.FindStringSubmatch(part); len(dm) > 0 {
				if dm[1] != "" {
					if s, err := strconv.Atoi(dm[1]); err == nil {
						res.Season = s
						break
					}
				} else if dm[2] != "" {
					res.Season = parseNumeral(dm[2])
					break
				}
			}
		}
	}

	if res.Season == 0 {
		res.Season = 1
	}
}

func extractYear(baseName string, parts []string) int64 {
	// 优先在文件名中寻找年份
	if ym := yearRegex.FindStringSubmatch(baseName); len(ym) > 1 {
		if y, err := strconv.ParseInt(ym[1], 10, 64); err == nil {
			return y
		}
	}

	// 回溯目录层级寻找年份（倒序优先靠近文件的层级）
	for i := len(parts) - 2; i >= 0; i-- {
		part := parts[i]
		if ym := yearRegex.FindStringSubmatch(part); len(ym) > 1 {
			if y, err := strconv.ParseInt(ym[1], 10, 64); err == nil {
				return y
			}
		}
	}

	return 0
}

func extractTitle(baseName string, parts []string, res *ParsedMedia) (string, string) {
	cleaned := cleanTitleCandidate(baseName)

	var parentCleaned string
	parentFrom := "parent"
	if len(parts) > 1 {
		parentDir := parts[len(parts)-2]
		if isSeasonDirectory(parentDir) || isNoiseDirectory(parentDir) {
			if len(parts) > 2 {
				parentCleaned = cleanTitleCandidate(parts[len(parts)-3])
				parentFrom = "grandparent"
			}
		} else {
			parentCleaned = cleanTitleCandidate(parentDir)
		}
	}

	fileOK := isValidTitle(cleaned)
	parentOK := isValidTitle(parentCleaned) && !isGenericMediaDir(parentCleaned)
	if fileOK && parentOK && !res.IsTV && preferFolderTitle(parentCleaned, cleaned) {
		return parentCleaned, parentFrom
	}
	if fileOK {
		return cleaned, "filename"
	}
	if parentOK {
		return parentCleaned, parentFrom
	}
	return "", "filename"
}

// cleanTitleCandidate 对片名候选串进行技术噪音与季集标记剥离
func extractTmdbID(s string) int64 {
	m := tmdbIDRegex.FindStringSubmatch(s)
	if len(m) < 2 {
		return 0
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func isGenericMediaDir(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "downloads", "download", "movies", "movie", "video", "videos", "media", "films", "film", "anime":
		return true
	}
	return false
}

func hanRatio(s string) float64 {
	han, total := 0, 0
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			han++
			total++
		} else if unicode.IsLetter(r) {
			total++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(han) / float64(total)
}

func preferFolderTitle(folder, file string) bool {
	if titleQuality(folder) > titleQuality(file) {
		return true
	}
	fr, fi := hanRatio(folder), hanRatio(file)
	return fr >= 0.6 && fr > fi
}

func titleQuality(s string) int {
	q := 0
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r):
			q += 4
		case unicode.IsLetter(r):
			q++
		}
	}
	lower := strings.ToLower(s)
	for _, junk := range []string{"bdrip", "bd ", "web-dl", "raws", "1080", "720", "x264", "x265", "flac", "hi10", "sc&tc", "movie ("} {
		if strings.Contains(lower, junk) {
			q -= 8
		}
	}
	q -= strings.Count(s, "[") * 5
	q -= strings.Count(s, "]") * 5
	q -= strings.Count(s, "(") * 2
	return q
}

func cleanTitleCandidate(raw string) string {
	s := raw
	for sitePrefixRegex.MatchString(s) {
		s = strings.TrimSpace(sitePrefixRegex.ReplaceAllString(s, ""))
	}
	s = tmdbIDRegex.ReplaceAllString(s, "")
	s = bracketGroupRegex.ReplaceAllString(s, " ")
	s = trailingMovieTag.ReplaceAllString(s, " (")
	s = parenSourceRegex.ReplaceAllString(s, "")

	// 剥离尾部噪音与技术参数
	s = noiseTokensRegex.ReplaceAllString(s, "")
	s = standaloneNoiseRegex.ReplaceAllString(s, "")

	// 剥离季集标记，例如 S01E01, E01-E03, S02, 第03集, CD1
	s = rangeDiscRegex.ReplaceAllString(s, "")
	s = seRegex.ReplaceAllString(s, "")
	s = epRegex.ReplaceAllString(s, "")
	s = sRegex.ReplaceAllString(s, "")
	s = cnEpisodeRegex.ReplaceAllString(s, "")
	s = discSplitRegex.ReplaceAllString(s, "")

	// 剥离年份（如 2019 或 (2019)）
	s = yearRegex.ReplaceAllString(s, "")

	// 清洗中文之间的连接点（鬼灭之刃.无限列车篇 -> 鬼灭之刃无限列车篇）
	s = hanDotHanRegex.ReplaceAllString(s, "${1}${2}")

	// 点号与下划线替换为空格（对于非中文连词如 Breaking.Bad -> Breaking Bad）
	s = strings.ReplaceAll(s, ".", " ")
	s = strings.ReplaceAll(s, "_", " ")

	// 清理多余空括号与边沿符号
	s = regexp.MustCompile(`\(\s*\)|\[\s*\]|【\s*】`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`^[\s·\-_/：:．。、,，\(\)\[\]]+|[\s·\-_/：:．。、,，\(\)\[\]]+$`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")

	return strings.TrimSpace(s)
}

func isValidTitle(s string) bool {
	if s == "" {
		return false
	}
	// 仅由纯数字组成（如 01, 02），不可单独作为影视标题
	if standaloneDigits.MatchString(s) {
		return false
	}
	// 纯规格独立噪音
	if standaloneNoiseRegex.MatchString(s) {
		return false
	}
	// 纯 Season 词
	if isSeasonDirectory(s) || isNoiseDirectory(s) {
		return false
	}
	return true
}

func isSeasonDirectory(name string) bool {
	return dirSeasonRegex.MatchString(name) || strings.EqualFold(name, "season")
}

func isNoiseDirectory(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return lower == "合集" || lower == "complete" || lower == "full.season" || lower == "specials" || lower == "sp"
}

// parseNumeral 支持阿拉伯数字与中文小写数字（最高支持到百）
func parseNumeral(s string) int {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}

	cnMap := map[rune]int{
		'零': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4,
		'五': 5, '六': 6, '七': 7, '八': 8, '九': 9,
	}

	val := 0
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '十' {
			if val == 0 {
				val = 10
			} else {
				val = val * 10
			}
		} else if d, ok := cnMap[r]; ok {
			if i+1 < len(runes) && runes[i+1] == '十' {
				val += d * 10
				i++
			} else {
				val += d
			}
		}
	}
	return val
}
