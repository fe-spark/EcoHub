package film

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"slices"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

const maxPlotExcludes = 5

func hasTextValue(column string) string {
	return fmt.Sprintf("(%s <> '' AND %s IS NOT NULL)", column, column)
}

func isUnknownTextValue(column string) string {
	return fmt.Sprintf("(%s = '' OR %s IS NULL)", column, column)
}

func ApplyCategoryFilter(query *gorm.DB, pid int64, cid int64) *gorm.DB {
	isUncategorized := cid == model.TagUncategorizedValue
	pid = support.ResolveCategoryID(pid)
	if cid > 0 {
		cid = support.ResolveCategoryID(cid)
	}
	switch {
	case isUncategorized && pid > 0:
		return query.Where("pid = ? AND cid = 0", pid)
	case cid > 0 && support.IsRootCategory(cid):
		return query.Where("pid = ?", cid)
	case cid > 0:
		return query.Where("cid = ?", cid)
	case pid > 0:
		return query.Where("pid = ?", pid)
	default:
		return query
	}
}

func resolveCategoryFieldValue(field string, id int64) (string, int64) {
	resolvedID := support.ResolveCategoryID(id)
	if field == "cid" {
		return "cid", resolvedID
	}
	return "pid", resolvedID
}

func buildCategoryQuery(field string, id int64) *gorm.DB {
	resolvedField, resolvedID := resolveCategoryFieldValue(field, id)
	return db.Mdb.Model(&model.SearchInfo{}).Where(fmt.Sprintf("%s = ?", resolvedField), resolvedID)
}

func applyPageStats(query *gorm.DB, page *dto.Page) *dto.Page {
	page = ensurePage(page)
	dto.GetPage(query, page)
	return page
}

func queryMovieListByCategory(field string, id int64, limit int, offset int) []model.MovieBasicInfo {
	var searchInfos []model.SearchInfo
	if err := buildCategoryQuery(field, id).
		Order(latestUpdateOrderSQL).
		Limit(limit).
		Offset(offset).
		Find(&searchInfos).Error; err != nil {
		log.Printf("queryMovieListByCategory Error: %v", err)
		return nil
	}
	return GetBasicInfoBySearchInfos(searchInfos...)
}

func queryHotMoviesByCategory(field string, id int64, limit int, offset int) []model.SearchInfo {
	var searchInfos []model.SearchInfo
	hotSince := time.Now().AddDate(0, -1, 0).Unix()
	if err := buildCategoryQuery(field, id).
		Where("update_stamp > ?", hotSince).
		Order("year DESC, hits DESC, mid DESC").
		Limit(limit).
		Offset(offset).
		Find(&searchInfos).Error; err != nil {
		log.Printf("queryHotMoviesByCategory Error: %v", err)
		return nil
	}
	return searchInfos
}

func applyMovieSortQuery(query *gorm.DB, sortType int) *gorm.DB {
	switch sortType {
	case 0:
		return query.Order("collect_stamp DESC, mid DESC")
	case 1:
		return query.Order("hits DESC, mid DESC")
	case 2:
		return query.Order(latestUpdateOrderSQL)
	default:
		return query.Order(latestUpdateOrderSQL)
	}
}

func queryMovieListBySort(pid int64, sortType int, limit int, offset int) []model.MovieBasicInfo {
	var searchInfos []model.SearchInfo
	query := applyMovieSortQuery(buildCategoryQuery("pid", pid), sortType)
	if err := query.Limit(limit).Offset(offset).Find(&searchInfos).Error; err != nil {
		log.Printf("queryMovieListBySort Error: %v", err)
		return nil
	}
	return GetBasicInfoBySearchInfos(searchInfos...)
}

// GetMovieListByPid 获取指定父类 ID 的影片基本信息
func GetMovieListByPid(pid int64, page *dto.Page) []model.MovieBasicInfo {
	page = applyPageStats(buildCategoryQuery("pid", pid), page)
	return GetMovieListByPidLimit(pid, page.PageSize, getPageOffset(page))
}

// GetMovieListByPidLimit 轻量级获取指定父类 ID 列表 (无 Count)
func GetMovieListByPidLimit(pid int64, limit, offset int) []model.MovieBasicInfo {
	return queryMovieListByCategory("pid", pid, limit, offset)
}

// GetMovieListByCid 获取指定子类 ID 的影片基本信息
func GetMovieListByCid(cid int64, page *dto.Page) []model.MovieBasicInfo {
	page = applyPageStats(buildCategoryQuery("cid", cid), page)
	return GetMovieListByCidLimit(cid, page.PageSize, getPageOffset(page))
}

// GetMovieListByCidLimit 轻量级获取指定子类 ID 列表 (无 Count)
func GetMovieListByCidLimit(cid int64, limit, offset int) []model.MovieBasicInfo {
	return queryMovieListByCategory("cid", cid, limit, offset)
}

func SearchFilmKeyword(keyword string, page *dto.Page) []model.SearchInfo {
	page = ensurePage(page)
	keywordQuery := buildNameKeywordQuery(keyword)
	var searchList []model.SearchInfo
	query := db.Mdb.Model(&model.SearchInfo{}).
		Where(keywordQuery).
		Order("year DESC, " + latestUpdateOrderSQL)

	dto.GetPage(query, page)
	query.Limit(page.PageSize).Offset(getPageOffset(page)).Find(&searchList)

	return searchList
}

func ensurePage(page *dto.Page) *dto.Page {
	if page == nil {
		return &dto.Page{Current: 1, PageSize: 20}
	}
	if page.Current <= 0 {
		page.Current = 1
	}
	if page.PageSize <= 0 {
		page.PageSize = 20
	}
	return page
}

func buildNameKeywordQuery(keyword string) *gorm.DB {
	keyword = strings.TrimSpace(keyword)
	keywordLike := fmt.Sprintf("%%%s%%", keyword)
	return db.Mdb.Where("name LIKE ? OR sub_title LIKE ?", keywordLike, keywordLike)
}

func getPageOffset(page *dto.Page) int {
	page = ensurePage(page)
	if page.Current <= 1 {
		return 0
	}
	return (page.Current - 1) * page.PageSize
}

func extractCoreSearchToken(name string) string {
	coreToken := strings.TrimSpace(name)
	if coreToken == "" {
		return ""
	}

	delimiters := []string{"：", ":", "·", " - ", "—", " ", "（", "(", "[", "【", "第", "剧场版", "部", "季", "之"}
	minIdx := len(coreToken)
	for _, delimiter := range delimiters {
		if idx := strings.Index(coreToken, delimiter); idx > 0 && idx < minIdx {
			minIdx = idx
		}
	}
	if minIdx < len(coreToken) {
		coreToken = strings.TrimSpace(coreToken[:minIdx])
	}
	coreToken = strings.TrimSpace(strings.TrimSuffix(coreToken, "年番"))
	for _, suffix := range []string{"特别篇", "篇章"} {
		coreToken = strings.TrimSpace(strings.TrimSuffix(coreToken, suffix))
	}
	for _, pattern := range []string{`(?i)tv\s*动画$`} {
		coreToken = strings.TrimSpace(regexp.MustCompile(pattern).ReplaceAllString(coreToken, ""))
	}

	runes := []rune(coreToken)
	nameRunes := []rune(strings.TrimSpace(name))
	if len(runes) >= 2 {
		return coreToken
	}
	if len(nameRunes) >= 4 {
		return string(nameRunes[:4])
	}
	if len(nameRunes) >= 2 {
		return string(nameRunes[:2])
	}
	return strings.TrimSpace(name)
}

func splitClassTags(classTag string) []string {
	normalized := strings.NewReplacer(" ", "", "/", ",", "|", ",", "，", ",").Replace(classTag)
	parts := strings.Split(normalized, ",")
	tags := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		tag := strings.TrimSpace(part)
		if tag == "" {
			continue
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	return tags
}

type relatedCandidateScore struct {
	Movie model.SearchInfo
	Score int
}

func splitAliasTitles(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := []string{raw}
	for _, sep := range []string{",", "，", "/", "|", "、"} {
		next := make([]string, 0, len(parts)*2)
		for _, part := range parts {
			if !strings.Contains(part, sep) {
				next = append(next, part)
				continue
			}
			for alias := range strings.SplitSeq(part, sep) {
				next = append(next, alias)
			}
		}
		parts = next
	}
	aliases := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		alias := strings.TrimSpace(part)
		if alias == "" {
			continue
		}
		if _, ok := seen[alias]; ok {
			continue
		}
		seen[alias] = struct{}{}
		aliases = append(aliases, alias)
	}
	return aliases
}

func buildTagSet(tags []string) map[string]struct{} {
	set := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		set[tag] = struct{}{}
	}
	return set
}

func appendUniqueRelatedCandidates(dst []model.SearchInfo, src []model.SearchInfo, seen map[int64]struct{}, limit int) []model.SearchInfo {
	for _, item := range src {
		if _, ok := seen[item.Mid]; ok {
			continue
		}
		seen[item.Mid] = struct{}{}
		dst = append(dst, item)
		if len(dst) >= limit {
			break
		}
	}
	return dst
}

func queryRelatedCandidates(search model.SearchInfo, limit int, apply func(query *gorm.DB) *gorm.DB) []model.SearchInfo {
	if limit <= 0 {
		return nil
	}
	query := db.Mdb.Model(&model.SearchInfo{}).
		Where("pid = ? AND mid != ?", search.Pid, search.Mid).
		Where("deleted_at IS NULL")
	if apply != nil {
		query = apply(query)
	}
	var list []model.SearchInfo
	if err := query.Order(latestUpdateOrderSQL).Limit(limit).Find(&list).Error; err != nil {
		log.Printf("queryRelatedCandidates Error: %v", err)
		return nil
	}
	return list
}

func loadRelatedCandidates(search model.SearchInfo, limit int) []model.SearchInfo {
	coreToken := extractCoreSearchToken(search.Name)
	tags := splitClassTags(search.ClassTag)
	list := make([]model.SearchInfo, 0, limit)
	seen := make(map[int64]struct{}, limit)
	strongLimit := max(limit, 20)
	cidLimit := max(limit/2, 10)
	tagLimit := max(limit/3, 6)

	if search.SeriesKey != "" {
		list = appendUniqueRelatedCandidates(list, queryRelatedCandidates(search, strongLimit, func(query *gorm.DB) *gorm.DB {
			return query.Where("series_key = ?", search.SeriesKey)
		}), seen, limit)
	}
	if coreToken != "" {
		like := fmt.Sprintf("%%%s%%", coreToken)
		list = appendUniqueRelatedCandidates(list, queryRelatedCandidates(search, strongLimit, func(query *gorm.DB) *gorm.DB {
			return query.Where("name LIKE ? OR sub_title LIKE ?", like, like)
		}), seen, limit)
	}
	if search.Cid > 0 {
		list = appendUniqueRelatedCandidates(list, queryRelatedCandidates(search, cidLimit, func(query *gorm.DB) *gorm.DB {
			return query.Where("cid = ?", search.Cid)
		}), seen, limit)
	}
	for _, tag := range tags {
		list = appendUniqueRelatedCandidates(list, queryRelatedCandidates(search, tagLimit, func(query *gorm.DB) *gorm.DB {
			return query.Where("class_tag LIKE ?", fmt.Sprintf("%%%s%%", tag))
		}), seen, limit)
	}
	return list
}

func calcTitleScore(coreToken string, candidate model.SearchInfo) int {
	coreToken = strings.TrimSpace(coreToken)
	if coreToken == "" {
		return 0
	}

	name := strings.TrimSpace(candidate.Name)
	subTitle := strings.TrimSpace(candidate.SubTitle)
	nameLike := strings.Contains(name, coreToken)
	prefixLike := strings.HasPrefix(name, coreToken)
	if name == coreToken {
		return 35
	}
	if prefixLike {
		return 25
	}
	if nameLike {
		return 15
	}
	if subTitle != "" && strings.Contains(subTitle, coreToken) {
		return 8
	}
	return 0
}

func calcAliasScore(current model.SearchInfo, candidate model.SearchInfo) int {
	aliases := splitAliasTitles(current.SubTitle)
	if len(aliases) == 0 {
		return 0
	}
	name := strings.TrimSpace(candidate.Name)
	subTitle := strings.TrimSpace(candidate.SubTitle)
	best := 0
	for _, alias := range aliases {
		score := 0
		switch {
		case alias == name:
			score = 20
		case strings.HasPrefix(name, alias):
			score = 14
		case strings.Contains(name, alias):
			score = 10
		case subTitle != "" && strings.Contains(subTitle, alias):
			score = 6
		}
		if score > best {
			best = score
		}
	}
	return best
}

func calcTagOverlapScore(currentTags, candidateTags []string) int {
	if len(currentTags) == 0 || len(candidateTags) == 0 {
		return 0
	}
	currentSet := buildTagSet(currentTags)
	score := 0
	for _, tag := range candidateTags {
		if _, ok := currentSet[tag]; ok {
			score += 8
			if score >= 24 {
				return 24
			}
		}
	}
	return score
}

func calcMetaScore(current, candidate model.SearchInfo) int {
	score := 0
	if current.Year > 0 && candidate.Year > 0 {
		diff := current.Year - candidate.Year
		if diff < 0 {
			diff = -diff
		}
		switch diff {
		case 0:
			score += 8
		case 1:
			score += 4
		}
	}
	if current.Area != "" && current.Area == candidate.Area {
		score += 5
	}
	if current.Language != "" && current.Language == candidate.Language {
		score += 3
	}
	return score
}

func freshnessBoost(candidate model.SearchInfo) int {
	stamp := candidate.LatestSourceStamp
	if stamp <= 0 {
		stamp = candidate.UpdateStamp
	}
	if stamp <= 0 {
		return 0
	}
	age := time.Now().Unix() - stamp
	switch {
	case age <= 7*24*3600:
		return 10
	case age <= 30*24*3600:
		return 6
	case age <= 90*24*3600:
		return 3
	default:
		return 0
	}
}

func scoreRelatedCandidate(current model.SearchInfo, candidate model.SearchInfo) relatedCandidateScore {
	score := 0
	if current.SeriesKey != "" && current.SeriesKey == candidate.SeriesKey {
		score += 80
	}
	if current.Cid > 0 && current.Cid == candidate.Cid {
		score += 40
	}
	score += calcTitleScore(extractCoreSearchToken(current.Name), candidate)
	score += calcAliasScore(current, candidate)
	score += calcTagOverlapScore(splitClassTags(current.ClassTag), splitClassTags(candidate.ClassTag))
	score += calcMetaScore(current, candidate)
	score += freshnessBoost(candidate)
	return relatedCandidateScore{Movie: candidate, Score: score}
}

func rankRelatedCandidates(current model.SearchInfo, candidates []model.SearchInfo, pageSize int) []model.SearchInfo {
	if len(candidates) == 0 || pageSize <= 0 {
		return nil
	}
	scored := make([]relatedCandidateScore, 0, len(candidates))
	seen := make(map[int64]struct{}, len(candidates))
	for _, candidate := range candidates {
		if _, ok := seen[candidate.Mid]; ok {
			continue
		}
		seen[candidate.Mid] = struct{}{}
		scored = append(scored, scoreRelatedCandidate(current, candidate))
	}
	slices.SortFunc(scored, func(a, b relatedCandidateScore) int {
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		if a.Movie.LatestSourceStamp != b.Movie.LatestSourceStamp {
			if a.Movie.LatestSourceStamp < b.Movie.LatestSourceStamp {
				return 1
			}
			return -1
		}
		if a.Movie.UpdateStamp != b.Movie.UpdateStamp {
			if a.Movie.UpdateStamp < b.Movie.UpdateStamp {
				return 1
			}
			return -1
		}
		if a.Movie.Mid < b.Movie.Mid {
			return 1
		}
		if a.Movie.Mid > b.Movie.Mid {
			return -1
		}
		return 0
	})
	if len(scored) > pageSize {
		scored = scored[:pageSize]
	}
	list := make([]model.SearchInfo, 0, len(scored))
	for _, item := range scored {
		list = append(list, item.Movie)
	}
	return list
}

func loadFallbackCandidates(search model.SearchInfo, limit int, exclude map[int64]struct{}) []model.SearchInfo {
	if limit <= 0 {
		return nil
	}
	appendUnique := func(dst []model.SearchInfo, source []model.SearchInfo, max int) []model.SearchInfo {
		for _, item := range source {
			if _, ok := exclude[item.Mid]; ok {
				continue
			}
			exclude[item.Mid] = struct{}{}
			dst = append(dst, item)
			if len(dst) >= max {
				break
			}
		}
		return dst
	}
	var result []model.SearchInfo
	if search.Cid > 0 {
		result = appendUnique(result, getFallbackRelatedSearchInfos(search, &dto.Page{Current: 1, PageSize: limit}), limit)
	}
	if len(result) >= limit || search.Pid <= 0 {
		return result
	}
	var pidHotList []model.SearchInfo
	hotSince := time.Now().AddDate(0, -1, 0).Unix()
	if err := db.Mdb.Model(&model.SearchInfo{}).
		Where("pid = ? AND mid != ?", search.Pid, search.Mid).
		Where("deleted_at IS NULL").
		Where("update_stamp > ?", hotSince).
		Order("year DESC, hits DESC, mid DESC").
		Limit(limit * 2).
		Find(&pidHotList).Error; err != nil {
		log.Printf("loadFallbackCandidates Pid Hot Fallback Error: %v", err)
	} else {
		result = appendUnique(result, pidHotList, limit)
	}
	if len(result) >= limit {
		return result
	}
	var pidList []model.SearchInfo
	if err := db.Mdb.Model(&model.SearchInfo{}).
		Where("pid = ? AND mid != ?", search.Pid, search.Mid).
		Where("deleted_at IS NULL").
		Order(latestUpdateOrderSQL).
		Limit(limit * 2).
		Find(&pidList).Error; err != nil {
		log.Printf("loadFallbackCandidates Pid Fallback Error: %v", err)
		return result
	}
	return appendUnique(result, pidList, limit)
}

func buildRelatedMovieQuery(search model.SearchInfo, coreToken string, tags []string) *gorm.DB {
	nameLike := fmt.Sprintf("%%%s%%", coreToken)
	prefixLike := fmt.Sprintf("%s%%", coreToken)
	escapedCoreToken := strings.ReplaceAll(coreToken, "'", "''")
	escapedPrefixLike := strings.ReplaceAll(prefixLike, "'", "''")
	escapedNameLike := strings.ReplaceAll(nameLike, "'", "''")

	query := db.Mdb.Model(&model.SearchInfo{}).
		Where("pid = ? AND mid != ?", search.Pid, search.Mid).
		Where("deleted_at IS NULL")

	nameCondition := db.Mdb.Where("name LIKE ? OR sub_title LIKE ?", nameLike, nameLike)
	for _, tag := range tags {
		nameCondition = nameCondition.Or("class_tag LIKE ?", fmt.Sprintf("%%%s%%", tag))
	}

	query = query.Where(nameCondition)
	query = query.Order(fmt.Sprintf("(name = '%s') DESC", escapedCoreToken))
	query = query.Order(fmt.Sprintf("(name LIKE '%s') DESC", escapedPrefixLike))
	query = query.Order(fmt.Sprintf("(name LIKE '%s' OR sub_title LIKE '%s') DESC", escapedNameLike, escapedNameLike))
	if search.Cid > 0 {
		query = query.Order(fmt.Sprintf("(cid = %d) DESC", search.Cid))
	}
	query = query.Order(latestUpdateOrderSQL)

	return query
}

func getFallbackRelatedSearchInfos(search model.SearchInfo, page *dto.Page) []model.SearchInfo {
	if search.Cid <= 0 {
		return nil
	}

	var list []model.SearchInfo
	if err := db.Mdb.Model(&model.SearchInfo{}).
		Where("cid = ? AND mid != ?", search.Cid, search.Mid).
		Where("deleted_at IS NULL").
		Order(latestUpdateOrderSQL).
		Offset(getPageOffset(page)).
		Limit(page.PageSize).
		Find(&list).Error; err != nil {
		log.Printf("GetRelateMovieBasicInfo Fallback Error: %v", err)
		return nil
	}
	return list
}

func GetRelateMovieBasicInfo(search model.SearchInfo, page *dto.Page) []model.MovieBasicInfo {
	page = ensurePage(page)
	targetSize := page.Current * page.PageSize
	candidates := loadRelatedCandidates(search, max(targetSize*5, 80))
	ranked := rankRelatedCandidates(search, candidates, targetSize)
	if len(ranked) < targetSize {
		exclude := make(map[int64]struct{}, len(ranked)+1)
		exclude[search.Mid] = struct{}{}
		for _, item := range ranked {
			exclude[item.Mid] = struct{}{}
		}
		fallback := loadFallbackCandidates(search, targetSize-len(ranked), exclude)
		ranked = append(ranked, fallback...)
	}
	offset := getPageOffset(page)
	if offset >= len(ranked) {
		return []model.MovieBasicInfo{}
	}
	end := min(offset+page.PageSize, len(ranked))
	return GetBasicInfoBySearchInfos(ranked[offset:end]...)
}

// GetBasicInfoByKey 获取影片的基本信息
func GetBasicInfoByKey(cid int64, mid int64) model.MovieBasicInfo {
	var info model.MovieDetailInfo
	if err := db.Mdb.Where("mid = ?", mid).First(&info).Error; err == nil {
		var detail model.MovieDetail
		_ = json.Unmarshal([]byte(info.Content), &detail)
		return model.MovieBasicInfo{
			Id: detail.Id, Cid: detail.Cid, Pid: detail.Pid, Name: detail.Name,
			SubTitle: detail.SubTitle, CName: detail.CName, State: detail.State,
			Picture: detail.Picture, Actor: detail.Actor, Director: detail.Director,
			Blurb: detail.Blurb, Remarks: detail.Remarks, Area: detail.Area, Year: detail.Year,
		}
	}
	return model.MovieBasicInfo{}
}

// GetMovieDetail 获取影片详情信息
func GetMovieDetail(cid int64, mid int64) *model.MovieDetail {
	var movieDetailInfo model.MovieDetailInfo
	if err := db.Mdb.Where("mid = ?", mid).First(&movieDetailInfo).Error; err != nil {
		log.Printf("GetMovieDetail Error: %v", err)
		return nil
	}
	var detail model.MovieDetail
	if err := json.Unmarshal([]byte(movieDetailInfo.Content), &detail); err != nil {
		log.Printf("Unmarshal MovieDetail Error: %v", err)
		return nil
	}

	if detail.PlayFrom == nil {
		detail.PlayFrom = []string{}
	}
	if detail.PlayList == nil {
		detail.PlayList = [][]model.MovieUrlInfo{}
	} else {
		for i, inner := range detail.PlayList {
			if inner == nil {
				detail.PlayList[i] = []model.MovieUrlInfo{}
			}
		}
	}
	if detail.DownloadList == nil {
		detail.DownloadList = [][]model.MovieUrlInfo{}
	} else {
		for i, inner := range detail.DownloadList {
			if inner == nil {
				detail.DownloadList[i] = []model.MovieUrlInfo{}
			}
		}
	}
	return &detail
}

func GetSearchPage(s model.SearchVo) []model.SearchInfo {
	page := ensurePage(s.Paging)

	query := applySearchPageFilters(db.Mdb.Model(&model.SearchInfo{}), s).Order(latestUpdateOrderSQL)

	dto.GetPage(query, page)
	var sl []model.SearchInfo
	if err := query.Limit(page.PageSize).Offset((page.Current - 1) * page.PageSize).Find(&sl).Error; err != nil {
		log.Printf("GetSearchPage Error: %v", err)
		return nil
	}
	return sl
}

func applySearchPageFilters(query *gorm.DB, s model.SearchVo) *gorm.DB {
	if s.Name != "" {
		query = query.Where("name LIKE ?", fmt.Sprintf("%%%s%%", s.Name))
	}

	query = ApplyCategoryFilter(query, s.Pid, s.Cid)

	if s.Plot != "" {
		query = query.Where("class_tag LIKE ?", fmt.Sprintf("%%%s%%", s.Plot))
	}
	if s.Area != "" {
		query = query.Where("area = ?", s.Area)
	}
	if s.Language != "" {
		query = query.Where("language = ?", s.Language)
	}
	if s.Year > 0 {
		query = query.Where("year = ?", s.Year)
	}

	if s.BeginTime > 0 {
		query = query.Where("update_stamp >= ?", s.BeginTime)
	}
	if s.EndTime > 0 {
		query = query.Where("update_stamp <= ?", s.EndTime)
	}

	return query
}

func applyYearTagFilter(query *gorm.DB, pid int64, fieldName string, value string) *gorm.DB {
	switch value {
	case model.TagOthersValue:
		topVals := GetTopTagValues(pid, fieldName)
		query = query.Where("year <> 0")
		if len(topVals) > 0 {
			query = query.Where("year NOT IN ?", topVals)
		}
		return query
	case model.TagUnknownValue:
		return query.Where("year = 0")
	default:
		return query.Where("year = ?", value)
	}
}

func applyTextTagFilter(query *gorm.DB, pid int64, fieldName string, column string, value string) *gorm.DB {
	switch value {
	case model.TagOthersValue:
		topVals := GetTopTagValues(pid, fieldName)
		query = query.Where(hasTextValue(column))
		if len(topVals) > 0 {
			query = query.Where(fmt.Sprintf("%s NOT IN ?", column), topVals)
		}
		return query
	case model.TagUnknownValue:
		return query.Where(isUnknownTextValue(column))
	default:
		return query.Where(fmt.Sprintf("%s = ?", column), value)
	}
}

func applyPlotTagFilter(query *gorm.DB, pid int64, fieldName string, value string) *gorm.DB {
	switch value {
	case model.TagOthersValue:
		topVals := GetTopTagValues(pid, fieldName)
		query = query.Where(hasTextValue("class_tag"))
		excludeCount := len(topVals)
		if excludeCount > maxPlotExcludes {
			excludeCount = maxPlotExcludes
		}
		for i := 0; i < excludeCount; i++ {
			query = query.Where("class_tag NOT LIKE ?", fmt.Sprintf("%%%v%%", topVals[i]))
		}
		return query
	case model.TagUnknownValue:
		return query.Where(isUnknownTextValue("class_tag"))
	default:
		return query.Where("class_tag LIKE ?", fmt.Sprintf("%%%v%%", value))
	}
}

func applySearchTagSort(query *gorm.DB, value string) *gorm.DB {
	if value == "" {
		value = "latest_source_stamp"
	}
	column, allowed := allowedSearchSortColumns[value]
	if !allowed {
		column = allowedSearchSortColumns["latest_source_stamp"]
	}
	if strings.EqualFold(column, "collect_stamp") {
		return query.Order("collect_stamp DESC, mid DESC")
	}
	if strings.EqualFold(column, "latest_source_stamp") {
		return query.Order(latestUpdateOrderSQL)
	}
	return query.Order(fmt.Sprintf("%s DESC, mid DESC", column))
}

func applySearchTagFilters(query *gorm.DB, st model.SearchTagsVO) *gorm.DB {
	query = ApplyCategoryFilter(query, st.Pid, st.Cid)

	if st.Year != "" {
		query = applyYearTagFilter(query, st.Pid, "Year", st.Year)
	}
	if st.Area != "" {
		query = applyTextTagFilter(query, st.Pid, "Area", "area", st.Area)
	}
	if st.Language != "" {
		query = applyTextTagFilter(query, st.Pid, "Language", "language", st.Language)
	}
	if st.Plot != "" {
		query = applyPlotTagFilter(query, st.Pid, "Plot", st.Plot)
	}

	return applySearchTagSort(query, st.Sort)
}

func GetSearchInfosByTags(st model.SearchTagsVO, page *dto.Page) []model.SearchInfo {
	page = ensurePage(page)
	st = normalizeSearchTagsVO(st)
	qw := applySearchTagFilters(db.Mdb.Model(&model.SearchInfo{}), st)

	dto.GetPage(qw, page)
	var sl []model.SearchInfo
	if err := qw.Limit(page.PageSize).Offset((page.Current - 1) * page.PageSize).Find(&sl).Error; err != nil {
		log.Printf("GetSearchInfosByTags Error: %v", err)
		return nil
	}
	return sl
}

func GetSearchInfoById(id int64) *model.SearchInfo {
	s := model.SearchInfo{}
	if err := db.Mdb.Where("mid = ?", id).First(&s).Error; err != nil {
		return nil
	}
	return &s
}

// GetHotMovieByPid 获取当前级分类下的热门影片
func GetHotMovieByPid(pid int64, page *dto.Page) []model.SearchInfo {
	page = ensurePage(page)
	return GetHotMovieByPidLimit(pid, page.PageSize, getPageOffset(page))
}

// GetHotMovieByPidLimit 轻量级获取热门影片
func GetHotMovieByPidLimit(pid int64, limit, offset int) []model.SearchInfo {
	return queryHotMoviesByCategory("pid", pid, limit, offset)
}

// GetHotMovieByCid 获取当前分类下的热门影片
func GetHotMovieByCid(cid int64, page *dto.Page) []model.SearchInfo {
	page = ensurePage(page)
	return GetHotMovieByCidLimit(cid, page.PageSize, getPageOffset(page))
}

// GetHotMovieByCidLimit 轻量级获取热门影片
func GetHotMovieByCidLimit(cid int64, limit, offset int) []model.SearchInfo {
	return queryHotMoviesByCategory("cid", cid, limit, offset)
}

// GetMovieListBySort 通过排序类型返回对应的影片基本信息
func GetMovieListBySort(t int, pid int64, page *dto.Page) []model.MovieBasicInfo {
	page = ensurePage(page)
	return queryMovieListBySort(pid, t, page.PageSize, getPageOffset(page))
}
