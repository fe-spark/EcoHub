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
	identityScoreDouban        = 100
	identityScoreNameExact     = 16
	identityScoreCategoryRoot  = 18
	identityScoreCategoryCName = 8
	identityScoreTagMax        = 24
	identityScoreYearExact     = 30
	identityScoreYearNear      = 12
	identityScoreRemarksKind   = 20

	identityConfirmMin    = 20
	identityConfirmMargin = 12

	// 无集号的单条线路对上这个规模以上的连载，视为不同作品（电影/合集 vs 长剧）。
	identitySingleVsSerialMin = 8
)

// IdentityProfile 跨站身份比对用的轻量字段，全部来自 film_index / 采集详情，不另查库。
type IdentityProfile struct {
	DbID      int64
	Name      string
	RootPid   int64
	CName     string
	ClassTag  string
	Year      int64
	Director  string
	Remarks   string
	Episodes  []model.MovieUrlInfo
}

// IdentityScore 各信号得分。Confirm = 豆瓣+年份+备注形态，不含片名、分类和通用标签。
type IdentityScore struct {
	Total    int
	Douban   int
	Name     int
	Category int
	Tag      int
	Year     int
	Remarks  int
}

func (s IdentityScore) Confirm() int {
	return s.Douban + s.Year + s.Remarks
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

func IdentityFromFilmListSnapshot(s model.FilmListSnapshot) IdentityProfile {
	return IdentityFromFilmIndex(model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: s.Mid, DbId: s.DbId},
		FilmIndexCategory: model.FilmIndexCategory{Pid: s.Pid, Cid: s.Cid, CName: s.CName},
		FilmIndexContent:  model.FilmIndexContent{Name: s.Name, ClassTag: s.ClassTag, Year: s.Year, Director: s.Director, Remarks: s.Remarks},
	})
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

func ScoreIdentity(slave, master IdentityProfile) IdentityScore {
	var score IdentityScore
	namesMatch := identityNamesMatch(slave.Name, master.Name)
	if slave.DbID > 0 && master.DbID > 0 && slave.DbID == master.DbID && namesMatch {
		score.Douban = identityScoreDouban
	}
	if namesMatch {
		score.Name = identityScoreNameExact
	}
	if slave.RootPid > 0 && master.RootPid > 0 && slave.RootPid == master.RootPid {
		score.Category += identityScoreCategoryRoot
	}
	if categoryNameRelated(slave.CName, master.CName) {
		score.Category += identityScoreCategoryCName
	}
	score.Tag = tagOverlapScore(slave.ClassTag, master.ClassTag)
	score.Year = yearIdentityScore(slave, master)
	score.Remarks = remarksIdentityScore(slave, master)
	score.Total = score.Douban + score.Name + score.Category + score.Tag + score.Year + score.Remarks
	return score
}

// CompatibleWorkShape 用线路结构判断是否像同一部，不靠站点自定义文案。
// 无集号的单条线路对不上长连载；末项带两段数字（打包）对不上单段数字且规模差开的逐集。
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

// CompatibleIdentity 只在两边都有值时才比；主站或副站为空就跳过，不当冲突。
// 豆瓣两边都有：不同则拒；相同仍要求片名一致，对不上就不是同一部。缺一边则再看年份、导演。标签太乱，不参与否决。
func CompatibleIdentity(master, slave IdentityProfile) bool {
	namesMatch := identityNamesMatch(master.Name, slave.Name)
	if master.DbID > 0 && slave.DbID > 0 {
		if master.DbID != slave.DbID {
			return false
		}
		if identityNamePresent(master.Name) && identityNamePresent(slave.Name) && !namesMatch {
			return false
		}
		if namesMatch {
			return true
		}
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

func uniqueLeadingScore(scores map[int64]IdentityScore, value func(IdentityScore) int) int64 {
	bestMid := int64(0)
	best := -1
	second := -1
	for mid, score := range scores {
		v := value(score)
		switch {
		case v > best:
			second = best
			best = v
			bestMid = mid
		case v == best:
			bestMid = 0
			second = v
		case v > second:
			second = v
		}
	}
	if bestMid > 0 && best >= identityConfirmMin && best-maxInt(second, 0) >= identityConfirmMargin {
		return bestMid
	}
	return 0
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

// PickUniqueIdentityMid 在召回候选里按身份分选择唯一主站 mid。
// 两边都有值且对不上的候选先丢掉（空值跳过）；剩下一部才宽松绑定（副站分类标错也能挂上）。
// 同名多部时：豆瓣+片名唯一命中优先；否则确认信号（豆瓣/年份/备注形态）拉开差距才绑定；
// 确认打平则允许唯一大类命中。
func PickUniqueIdentityMid(slave IdentityProfile, candidates map[int64]IdentityProfile) int64 {
	if len(candidates) == 0 {
		return 0
	}
	compatible := make(map[int64]IdentityProfile, len(candidates))
	for mid, master := range candidates {
		if CompatibleIdentity(master, slave) && CompatibleWorkShape(master, slave) {
			compatible[mid] = master
		}
	}
	if len(compatible) == 0 {
		return 0
	}
	candidates = compatible
	if len(candidates) == 1 {
		for mid := range candidates {
			return mid
		}
	}

	scores := make(map[int64]IdentityScore, len(candidates))
	for mid, master := range candidates {
		scores[mid] = ScoreIdentity(slave, master)
	}

	dbHits := make([]int64, 0, 1)
	for mid, score := range scores {
		if score.Douban > 0 {
			dbHits = append(dbHits, mid)
		}
	}
	if len(dbHits) == 1 {
		return dbHits[0]
	}

	if mid := uniqueLeadingScore(scores, func(s IdentityScore) int { return s.Confirm() }); mid > 0 {
		return mid
	}

	catHits := make([]int64, 0, 1)
	for mid, master := range candidates {
		if slave.RootPid > 0 && master.RootPid > 0 && slave.RootPid == master.RootPid {
			catHits = append(catHits, mid)
		}
	}
	if len(catHits) == 1 {
		mid := catHits[0]
		for other, score := range scores {
			if other != mid && score.Confirm() > scores[mid].Confirm() {
				return 0
			}
		}
		return mid
	}
	return 0
}

func categoryNameRelated(left, right string) bool {
	a := strings.TrimSpace(left)
	b := strings.TrimSpace(right)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	return strings.Contains(a, b) || strings.Contains(b, a)
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

func yearIdentityScore(slave, master IdentityProfile) int {
	if slave.Year <= 0 || master.Year <= 0 {
		return 0
	}
	sk, mk := classifyRemarkKind(slave), classifyRemarkKind(master)
	if sk != remarkUnknown && mk != remarkUnknown && sk != mk {
		return 0
	}
	if sk == remarkSerial && mk == remarkSerial {
		slaveLast, masterLast := lastIdentityLabel(slave), lastIdentityLabel(master)
		if slaveLast != "" && masterLast != "" && slaveLast != masterLast {
			return 0
		}
	}
	diff := slave.Year - master.Year
	if diff < 0 {
		diff = -diff
	}
	switch diff {
	case 0:
		return identityScoreYearExact
	case 1:
		return identityScoreYearNear
	default:
		return 0
	}
}

func remarksIdentityScore(slave, master IdentityProfile) int {
	slaveLast := lastIdentityLabel(slave)
	masterLast := lastIdentityLabel(master)
	if slaveLast != "" && masterLast != "" && slaveLast == masterLast {
		return identityScoreRemarksKind
	}
	if classifyRemarkKind(slave) == remarkComplete && classifyRemarkKind(master) == remarkComplete {
		return identityScoreRemarksKind
	}
	return 0
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

func tagOverlapScore(left, right string) int {
	a := splitIdentityTags(left)
	b := splitIdentityTags(right)
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for tag := range a {
		if _, ok := b[tag]; ok {
			inter++
		}
	}
	if inter == 0 {
		return 0
	}
	union := len(a)
	for tag := range b {
		if _, ok := a[tag]; !ok {
			union++
		}
	}
	if union <= 0 {
		return 0
	}
	return identityScoreTagMax * inter / union
}

func splitIdentityTags(raw string) map[string]struct{} {
	raw = strings.NewReplacer(",", " ", "，", " ", "、", " ", "/", " ", "|", " ", ";", " ", "；", " ").Replace(raw)
	out := make(map[string]struct{})
	for _, part := range strings.Fields(raw) {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		out[part] = struct{}{}
	}
	return out
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
