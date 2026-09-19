package shared

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"server/internal/model"
	"server/internal/repository/support"
	"server/internal/utils"
)

const (
	// 无集号的单条线路对上这个规模以上的连载，视为不同作品（电影/合集 vs 长剧）。
	identitySingleVsSerialMin = 8
)

// IdentityProfile 跨站身份比对用的轻量字段，全部来自 film_index / 采集详情，不另查库。
type IdentityProfile struct {
	DbID     int64
	Name     string
	RootPid  int64
	CName    string
	ClassTag string
	Year     int64
	Director string
	Remarks  string
	Episodes []model.MovieUrlInfo
}

type remarkKind int

const (
	remarkUnknown remarkKind = iota
	remarkSerial
	remarkComplete
)

var (
	yearTokenRe      = regexp.MustCompile(`(?:19|20)\d{2}`)
	serialRemarkRe   = regexp.MustCompile(`第\s*[0-9一二三四五六七八九十百千万]+\s*[集期话回]|更新至|更新到|更至|连载`)
	completeRemarkRe = regexp.MustCompile(`全集|完结|合全集|已完结|全[0-9一二三四五六七八九十百千万]+[集期话回]|合集完结`)
	digitRunRe       = regexp.MustCompile(`\d+`)
	techNoiseRe      = regexp.MustCompile(`(?i)(?:1080[pi]?|720[pi]?|2160[pi]?|4k|\d+帧|\d+fps|5\.1声道?)`)
	packedRangeRe    = regexp.MustCompile(`(\d+)\s*[-~至到]\s*(\d+)`)
	serialProgressRe = regexp.MustCompile(`(?:更新至|更新到|更至|第)\s*(\d+)\s*[集期话回]?`)
)

func IdentityFromFilmIndex(info model.FilmIndex) IdentityProfile {
	pid := int64(0)
	if info.Pid > 0 {
		if root := support.GetRootId(info.Pid); root > 0 {
			pid = root
		}
	}
	if pid <= 0 && info.Cid > 0 {
		if root := support.GetRootId(info.Cid); root > 0 {
			pid = root
		}
	}
	if pid <= 0 {
		pid = support.ResolveRootCategoryIDByCName(info.CName)
	}
	return IdentityProfile{
		DbID:     info.DbId,
		Name:     info.Name,
		RootPid:  pid,
		CName:    info.CName,
		ClassTag: info.ClassTag,
		Year:     info.Year,
		Director: info.Director,
		Remarks:  info.Remarks,
	}
}

func IdentityFromMovieDetail(detail model.MovieDetail) IdentityProfile {
	episodes := make([]model.MovieUrlInfo, 0)
	for _, group := range detail.PlayList {
		episodes = append(episodes, group...)
	}
	return IdentityProfile{
		DbID:     detail.DbId,
		Name:     detail.Name,
		RootPid:  ResolveMovieDetailRootPid(detail),
		CName:    detail.CName,
		ClassTag: detail.ClassTag,
		Year:     ParseIdentityYear(detail.Year),
		Director: detail.Director,
		Remarks:  detail.Remarks,
		Episodes: episodes,
	}
}

func ParseIdentityYear(raw string) int64 {
	token := yearTokenRe.FindString(strings.TrimSpace(raw))
	if token == "" {
		return 0
	}
	year, err := strconv.ParseInt(token, 10, 64)
	if err != nil {
		return 0
	}
	return year
}

// CompatibleWorkShape 剧集结构一票否决：两边都有可识别结构且对不上才拒。
// 无集号的单条线路对不上长连载；末项带两段数字（打包）对不上单段数字且规模差开的逐集。
// 任一边空（无线路、无备注数字）不否决。
func CompatibleWorkShape(master, slave IdentityProfile) bool {
	masterItems := identityItemCount(master)
	masterNum, masterPacked := identityEpisodeMeta(master)
	masterScale := maxInt64(masterNum, int64(masterItems))
	masterSingle := unnumberedSingle(masterItems, masterNum)
	return CompatibleWorkShapePrecomputed(masterNum, masterPacked, masterScale, masterSingle, slave)
}

// CompatibleWorkShapePrecomputed 提供高频/批量场景下的预计算快路径，避免循环内部重复解析主站元数据与正则计算。
func CompatibleWorkShapePrecomputed(masterNum int64, masterPacked bool, masterScale int64, masterSingle bool, slave IdentityProfile) bool {
	slaveItems := identityItemCount(slave)
	// 百万级优化快路径：主站与副站均非长剧（海量电影高频场景），直接放行，0 次正则
	if masterScale < identitySingleVsSerialMin && slaveItems <= 1 {
		return true
	}
	slaveNum, slavePacked := identityEpisodeMeta(slave)
	slaveScale := maxInt64(slaveNum, int64(slaveItems))

	if unnumberedSingle(slaveItems, slaveNum) && masterScale >= identitySingleVsSerialMin {
		return false
	}
	if masterSingle && slaveScale >= identitySingleVsSerialMin {
		return false
	}
	if masterNum > 0 && slaveNum > 0 && masterPacked != slavePacked {
		diff := masterNum - slaveNum
		if diff < 0 {
			diff = -diff
		}
		return diff <= 2
	}
	return true
}

// CompatibleIdentity 豆瓣 / 年份 / 导演一票否决：只在两边都有值时才比，空或未知跳过。
// 片名两边都能折出身份名却对不上，也否决。标签太乱，不参与否决。
func CompatibleIdentity(master, slave IdentityProfile) bool {
	if identityNamePresent(master.Name) && identityNamePresent(slave.Name) && !identityNamesMatch(master.Name, slave.Name) {
		return false
	}
	if master.DbID > 0 && slave.DbID > 0 && master.DbID != slave.DbID {
		return false
	}
	if master.Year > 0 && slave.Year > 0 {
		diff := master.Year - slave.Year
		if diff < 0 {
			diff = -diff
		}
		if diff > 1 {
			return false
		}
	}
	return tokenSetsCompatible(splitPersonNames(master.Director), splitPersonNames(slave.Director))
}

func tokenSetsCompatible(a, b map[string]struct{}) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	for name := range a {
		if _, ok := b[name]; ok {
			return true
		}
	}
	return false
}

func splitPersonNames(raw string) map[string]struct{} {
	raw = utils.TraditionalToSimplified(strings.TrimSpace(raw))
	if raw == "" {
		return nil
	}
	raw = strings.NewReplacer(
		",", " ", "，", " ",
		"、", " ", "/", " ",
		"|", " ", ";", " ", "；", " ",
		":", " ", "：", " ",
	).Replace(raw)
	out := make(map[string]struct{})
	for _, part := range strings.Fields(raw) {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" || part == "导演" || part == "执导" {
			continue
		}
		out[part] = struct{}{}
		withoutDot := strings.ReplaceAll(part, "·", "")
		if withoutDot != "" {
			out[withoutDot] = struct{}{}
		}
	}
	return out
}

func identityNamesMatch(left, right string) bool {
	a := utils.NormalizeIdentityTitle(left)
	b := utils.NormalizeIdentityTitle(right)
	return a != "" && a == b
}

func identityNamePresent(name string) bool {
	return utils.NormalizeIdentityTitle(name) != ""
}

func identityItemCount(profile IdentityProfile) int {
	n := 0
	for _, episode := range profile.Episodes {
		if strings.TrimSpace(episode.Episode) != "" || strings.TrimSpace(episode.Link) != "" {
			n++
		}
	}
	return n
}

func unnumberedSingle(items int, num int64) bool {
	return num <= 0 && items == 1
}

func identityEpisodeMeta(profile IdentityProfile) (num int64, packed bool) {
	label := lastIdentityLabel(profile)
	if strings.TrimSpace(label) == "" {
		return 0, false
	}
	cleaned := strings.TrimSpace(techNoiseRe.ReplaceAllString(label, " "))
	if cleaned == "" {
		return 0, false
	}

	// 1. 优先检测是否为打包区间（短剧打包特征：如 第81-106集完结、1-20集、1~30）
	if m := packedRangeRe.FindStringSubmatch(cleaned); len(m) == 3 {
		endNum, err := strconv.ParseInt(m[2], 10, 64)
		if err == nil && endNum > 0 {
			return endNum, true
		}
	}

	// 2. 检测更新进度（如 更新至10集/共30集、第287集）
	if m := serialProgressRe.FindStringSubmatch(cleaned); len(m) == 2 {
		epNum, err := strconv.ParseInt(m[1], 10, 64)
		if err == nil && epNum > 0 {
			return epNum, false
		}
	}

	// 3. 通用提取纯数字（忽略年份与8位日期）
	nums := parseLabelNumbers(cleaned)
	if len(nums) == 0 {
		return 0, false
	}
	return nums[0], false
}

func parseLabelNumbers(label string) []int64 {
	cleaned := techNoiseRe.ReplaceAllString(label, " ")
	raw := digitRunRe.FindAllString(strings.TrimSpace(cleaned), -1)
	if len(raw) == 0 {
		return nil
	}
	out := make([]int64, 0, len(raw))
	for _, token := range raw {
		n, err := strconv.ParseInt(token, 10, 64)
		if err != nil || n <= 0 {
			continue
		}
		if len(token) == 4 && n >= 1900 && n <= 2100 {
			continue
		}
		if len(token) == 8 && (strings.HasPrefix(token, "19") || strings.HasPrefix(token, "20")) {
			continue
		}
		out = append(out, n)
	}
	return out
}

// PickUniqueIdentityMid 规范化片名必须一致；豆瓣/年份/导演/剧集空值不否决，两边有值且冲突才拒。
// 否决后只剩一部就绑；同名多部按豆瓣 → 大类 → 年份选出唯一一部，选不出就不绑。
func PickUniqueIdentityMid(slave IdentityProfile, candidates map[int64]IdentityProfile) int64 {
	if len(candidates) == 0 {
		return 0
	}
	named := make(map[int64]IdentityProfile, len(candidates))
	for mid, master := range candidates {
		if !identityNamesMatch(slave.Name, master.Name) {
			continue
		}
		if CompatibleIdentity(master, slave) && CompatibleWorkShape(master, slave) {
			named[mid] = master
		}
	}
	if len(named) == 0 {
		return 0
	}
	if len(named) == 1 {
		for mid := range named {
			return mid
		}
	}

	if slave.DbID > 0 {
		if mid := uniqueProfileHit(named, func(master IdentityProfile) bool {
			return master.DbID > 0 && master.DbID == slave.DbID
		}); mid > 0 {
			return mid
		}
	}
	if slave.RootPid > 0 {
		if mid := uniqueProfileHit(named, func(master IdentityProfile) bool {
			return master.RootPid > 0 && master.RootPid == slave.RootPid
		}); mid > 0 {
			return mid
		}
	}
	if slave.Year > 0 {
		if mid := uniqueProfileHit(named, func(master IdentityProfile) bool {
			return master.Year > 0 && master.Year == slave.Year
		}); mid > 0 {
			return mid
		}
		if mid := uniqueProfileHit(named, func(master IdentityProfile) bool {
			if master.Year <= 0 {
				return false
			}
			diff := master.Year - slave.Year
			if diff < 0 {
				diff = -diff
			}
			return diff <= 1
		}); mid > 0 {
			return mid
		}
	}
	return 0
}

func uniqueProfileHit(candidates map[int64]IdentityProfile, match func(IdentityProfile) bool) int64 {
	hit := int64(0)
	for mid, master := range candidates {
		if !match(master) {
			continue
		}
		if hit > 0 {
			return 0
		}
		hit = mid
	}
	return hit
}

func lastIdentityLabel(profile IdentityProfile) string {
	last := ""
	for _, episode := range profile.Episodes {
		if label := strings.TrimSpace(episode.Episode); label != "" {
			last = label
		}
	}
	if last != "" {
		return last
	}
	return strings.TrimSpace(profile.Remarks)
}

// SameWorkPlaylist 已有线路和本次采集是否像同一部（末集标签相同，或都是全集/合全集）。
func SameWorkPlaylist(existing []model.MovieUrlInfo, incoming IdentityProfile) bool {
	got := IdentityProfile{Episodes: existing}
	a, b := lastIdentityLabel(got), lastIdentityLabel(incoming)
	if a != "" && a == b {
		return true
	}
	return classifyRemarkKind(got) == remarkComplete && classifyRemarkKind(incoming) == remarkComplete
}

func classifyRemarkKind(profile IdentityProfile) remarkKind {
	last := ""
	for _, episode := range profile.Episodes {
		if label := strings.TrimSpace(episode.Episode); label != "" {
			last = label
		}
	}
	text := strings.TrimSpace(profile.Remarks)
	if last != "" {
		if kind := remarkKindFromText(last); kind != remarkUnknown {
			return kind
		}
		text = text + " " + last
	}
	return remarkKindFromText(text)
}

func remarkKindFromText(text string) remarkKind {
	text = strings.TrimSpace(text)
	if text == "" {
		return remarkUnknown
	}
	complete := completeRemarkRe.MatchString(text)
	serial := serialRemarkRe.MatchString(text)
	switch {
	case complete && !serial:
		return remarkComplete
	case serial && !complete:
		return remarkSerial
	case complete && serial:
		if completeRemarkRe.MatchString(lastRuneWord(text)) {
			return remarkComplete
		}
		return remarkSerial
	default:
		return remarkUnknown
	}
}

func lastRuneWord(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return ""
	}
	end := len(runes)
	for end > 0 && unicode.IsSpace(runes[end-1]) {
		end--
	}
	start := end
	for start > 0 && !unicode.IsSpace(runes[start-1]) {
		start--
	}
	return string(runes[start:end])
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
