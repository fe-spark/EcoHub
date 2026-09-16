package playlist

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"server/internal/model"
	shared "server/internal/repository/film/shared"
)

var (
	yearExtractRegex     = regexp.MustCompile(`\b(19\d\d|20\d\d)\b`)
	nameSplitRegex       = regexp.MustCompile(`[,，、/|\\;\s]+`)
	episodeRemarksRegex  = regexp.MustCompile(`(?:更新至|更新到|全|共|第)?\s*(\d+)\s*(?:集|话|期|回)`)
	movieResolutionRegex = regexp.MustCompile(`(?i)\b(1080p|720p|4k|2160p|hd|bd|tc|ts|dvd|web-dl|蓝光|超清|高清|枪版)\b`)
)

// placeholderWords 占位或无意义废词
var placeholderWords = map[string]struct{}{
	"":   {},
	"暂无": {},
	"未知": {},
	"不详": {},
	"其他": {},
}

// ScoreSlaveCandidate 通用多维元数据匹配打分器（适用于电影、剧集、动漫、短剧、综艺、纪录片等所有大类）。
// 不包含任何具体分类名称的硬编码，纯靠各维度元数据的通用相似度数学特征判定。
func ScoreSlaveCandidate(slave model.MovieDetail, master model.FilmIndex) int {
	// 1. 外部权威唯一标识（如豆瓣 ID）：双方均存在时直接绝对定性
	if slave.DbId > 0 && master.DbId > 0 {
		if slave.DbId == master.DbId {
			return 100
		}
		return -100 // 外部 ID 明确不同，必为不同作品
	}

	score := 0

	// 2. 演职员与主创匹配（演员、配音、主持、导演等通用主创名单）
	actorMatches := countNameIntersections(slave.Actor, master.Actor)
	if actorMatches >= 2 {
		score += 50
	} else if actorMatches == 1 {
		score += 25
	}

	directorMatches := countNameIntersections(slave.Director, master.Director)
	if directorMatches >= 1 {
		score += 30
	}

	// 3. 上映/发行年份数值差（通用物理时间特征，分级惩罚）
	slaveYear := parseYearNumber(slave.Year)
	masterYear := int(master.Year)
	if slaveYear > 0 && masterYear > 0 {
		diff := slaveYear - masterYear
		if diff < 0 {
			diff = -diff
		}
		if diff == 0 {
			score += 25
		} else if diff == 1 {
			score += 15
		} else if diff == 2 {
			score -= 15
		} else if diff >= 3 && diff < 5 {
			score -= 30
		} else if diff >= 5 {
			score -= 50 // 跨年代强互斥
		}
	}

	// 4. 分类名称与标签通用文本相似度（通用字符 N-gram / Jaccard 重合度，0 硬编码分类词）
	catSimilarity := computeTextSimilarity(slave.CName, master.CName)
	if catSimilarity >= 0.5 {
		score += 25
	} else if catSimilarity > 0 {
		score += 10
	} else if len([]rune(strings.TrimSpace(slave.CName))) >= 2 && len([]rune(strings.TrimSpace(master.CName))) >= 2 {
		// 双方均有明确分类文本，但完全没有任何共同词根或字符关联，施加分类异质惩罚
		score -= 30
	}

	// 5. 分类标签 (ClassTag) 交叉相似度（如有）
	if slave.ClassTag != "" && master.ClassTag != "" {
		tagSim := computeTextSimilarity(slave.ClassTag, master.ClassTag)
		if tagSim >= 0.5 {
			score += 20
		} else if tagSim > 0 {
			score += 10
		}
	}

	// 6. 系统分类树根分类 ID（若已解析出根大类）
	slavePid := shared.ResolveMovieDetailRootPid(slave)
	masterPid := filmIndexRootPid(master)
	if slavePid > 0 && masterPid > 0 {
		if slavePid == masterPid {
			score += 30
		} else {
			score -= 50
		}
	}

	// 7. 地区/制片国家通用交集
	if slave.Area != "" && master.Area != "" {
		areaSim := computeTextSimilarity(slave.Area, master.Area)
		if areaSim > 0 {
			score += 10
		}
	}

	// 8. 播出形态一致性（单片 vs 连载集数特征）
	slaveEpisodes := countSlaveEpisodes(slave)
	masterEpisodes := countMasterEpisodes(master)
	if slaveEpisodes > 0 && masterEpisodes > 0 {
		slaveIsSerial := slaveEpisodes > 1
		masterIsSerial := masterEpisodes > 1
		if slaveIsSerial == masterIsSerial {
			score += 10
		} else if (slaveEpisodes >= 10 && masterEpisodes == 1) || (slaveEpisodes == 1 && masterEpisodes >= 10) {
			// 一方为几十集的连载剧/短剧/动漫，另一方为单集电影形态，结构互斥
			score -= 35
		}
	}

	return score
}

// pickBestMidByScore 多候选通用消歧裁决。
// 必须满足基本置信度 (score >= 40) 且显著领先第二名 (margin >= 20)。
func pickBestMidByScore(detail model.MovieDetail, candidateMids []int64, infoByMid map[int64]model.FilmIndex) int64 {
	if len(candidateMids) == 0 {
		return 0
	}
	if len(candidateMids) == 1 {
		info, ok := infoByMid[candidateMids[0]]
		if ok && ScoreSlaveCandidate(detail, info) >= 20 {
			return candidateMids[0]
		}
		return 0
	}

	bestMid := int64(0)
	bestScore := -9999
	secondScore := -9999

	for _, mid := range candidateMids {
		info, ok := infoByMid[mid]
		if !ok {
			continue
		}
		score := ScoreSlaveCandidate(detail, info)
		if score > bestScore {
			secondScore = bestScore
			bestScore = score
			bestMid = mid
		} else if score > secondScore {
			secondScore = score
		}
	}

	// 必须满足置信门槛与领先分差
	if bestMid > 0 && bestScore >= 40 && (bestScore-secondScore) >= 20 {
		return bestMid
	}

	return 0
}

func parseYearNumber(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	m := yearExtractRegex.FindString(raw)
	if m == "" {
		return 0
	}
	y, _ := strconv.Atoi(m)
	return y
}

func countNameIntersections(a, b string) int {
	namesA := splitAndCleanNames(a)
	if len(namesA) == 0 {
		return 0
	}
	namesB := splitAndCleanNames(b)
	if len(namesB) == 0 {
		return 0
	}

	setB := make(map[string]struct{}, len(namesB))
	for _, n := range namesB {
		setB[n] = struct{}{}
	}

	count := 0
	for _, n := range namesA {
		if _, ok := setB[n]; ok {
			count++
		}
	}
	return count
}

func splitAndCleanNames(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := nameSplitRegex.Split(raw, -1)
	uniq := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if _, invalid := placeholderWords[p]; invalid {
			continue
		}
		if utf8.RuneCountInString(p) < 2 {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		uniq = append(uniq, p)
	}
	return uniq
}

// computeTextSimilarity 计算两个短文本（分类名、标签等）的字符 2-gram 相似度（0.0 ~ 1.0）。
// 纯基于 N-gram 集合交并比，完全不依赖硬编码字典，自动识别“都市短剧/爽文短剧”、“国产动漫/日本动漫”等核心词组。
func computeTextSimilarity(s1, s2 string) float64 {
	r1 := cleanRunes(s1)
	r2 := cleanRunes(s2)
	if len(r1) == 0 || len(r2) == 0 {
		return 0.0
	}

	if string(r1) == string(r2) {
		return 1.0
	}

	grams1 := makeBigrams(r1)
	grams2 := makeBigrams(r2)

	if len(grams1) > 0 && len(grams2) > 0 {
		inter := 0
		set2 := make(map[string]struct{}, len(grams2))
		for _, g := range grams2 {
			set2[g] = struct{}{}
		}
		for _, g := range grams1 {
			if _, ok := set2[g]; ok {
				inter++
			}
		}
		union := len(grams1) + len(grams2) - inter
		if union > 0 && inter > 0 {
			return float64(inter) / float64(union)
		}
		return 0.0
	}

	// 仅当某一文本只有 1 个字符时回退单字匹配
	if len(r1) == 1 && len(r2) == 1 && r1[0] == r2[0] {
		return 1.0
	}
	return 0.0
}

func makeBigrams(runes []rune) []string {
	if len(runes) < 2 {
		return nil
	}
	grams := make([]string, 0, len(runes)-1)
	for i := 0; i < len(runes)-1; i++ {
		grams = append(grams, string(runes[i:i+2]))
	}
	return grams
}

func cleanRunes(s string) []rune {
	s = strings.ToLower(strings.TrimSpace(s))
	cleaned := make([]rune, 0, len(s))
	for _, r := range s {
		if r == ' ' || r == '/' || r == ',' || r == '，' || r == '、' || r == '-' || r == '_' || r == '|' {
			continue
		}
		cleaned = append(cleaned, r)
	}
	return cleaned
}

func countSlaveEpisodes(detail model.MovieDetail) int {
	maxCount := 0
	for _, links := range detail.PlayList {
		if len(links) > maxCount {
			maxCount = len(links)
		}
	}
	return maxCount
}

func countMasterEpisodes(info model.FilmIndex) int {
	rem := strings.TrimSpace(info.Remarks)
	if rem == "" {
		return 0
	}
	// 优先匹配分集量词（如 "更新至25集"、"全80集"、"第12期"）
	if m := episodeRemarksRegex.FindStringSubmatch(rem); len(m) >= 2 {
		if val, err := strconv.Atoi(m[1]); err == nil && val > 0 {
			return val
		}
	}
	// 若显式包含电影分辨率/版本词且无分集量词，判定为单片形态（集数 = 1）
	if movieResolutionRegex.MatchString(rem) {
		return 1
	}
	return 0
}
