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
)

// IdentityProfile 跨站身份比对用的轻量字段，全部来自 film_index / 采集详情，不另查库。
type IdentityProfile struct {
	DbID     int64
	Name     string
	RootPid  int64
	CName    string
	ClassTag string
	Year     int64
	Remarks  string
	Episodes []model.MovieUrlInfo
}

// IdentityScore 各信号得分。Confirm = 豆瓣+标签+年份+备注形态，不含片名和分类。
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
	return s.Douban + s.Tag + s.Year + s.Remarks
}

type remarkKind int

const (
	remarkUnknown remarkKind = iota
	remarkSerial
	remarkComplete
)

var (
	yearTokenRe      = regexp.MustCompile(`(?:19|20)\d{2}`)
	serialRemarkRe   = regexp.MustCompile(`第\s*[0-9一二三四五六七八九十百千万]+\s*集|更新至`)
	completeRemarkRe = regexp.MustCompile(`全集|完结|合全集`)
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

func ScoreIdentity(slave, master IdentityProfile) IdentityScore {
	var score IdentityScore
	if slave.DbID > 0 && master.DbID > 0 && slave.DbID == master.DbID {
		score.Douban = identityScoreDouban
	}
	slaveName := utils.NormalizeCollectionTitle(slave.Name)
	masterName := utils.NormalizeCollectionTitle(master.Name)
	if slaveName != "" && slaveName == masterName {
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

// PickUniqueIdentityMid 在召回候选里按身份分选择唯一主站 mid。
// 只有一部时保持宽松绑定（副站分类标错也能挂上）。
// 同名多部时：豆瓣唯一命中优先；否则确认信号（豆瓣/标签/年份/备注形态）拉开差距才绑定；
// 确认信号打平则允许唯一大类命中，但确认信号指向另一部时不绑。单靠分类、又无确认信号时仍可按大类绑定正确标注的源。
func PickUniqueIdentityMid(slave IdentityProfile, candidates map[int64]IdentityProfile) int64 {
	if len(candidates) == 0 {
		return 0
	}
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

	bestConfirmMid := int64(0)
	bestConfirm := -1
	secondConfirm := -1
	for mid, score := range scores {
		c := score.Confirm()
		switch {
		case c > bestConfirm:
			secondConfirm = bestConfirm
			bestConfirm = c
			bestConfirmMid = mid
		case c == bestConfirm:
			bestConfirmMid = 0
			secondConfirm = c
		case c > secondConfirm:
			secondConfirm = c
		}
	}
	if bestConfirmMid > 0 && bestConfirm >= identityConfirmMin && bestConfirm-maxInt(secondConfirm, 0) >= identityConfirmMargin {
		return bestConfirmMid
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
