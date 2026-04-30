package film

import (
	"encoding/json"
	"errors"
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

func categoryKeyColumn(field string) string {
	if field == "cid" {
		return "category_key"
	}
	return "root_category_key"
}

func categoryIDColumn(field string) string {
	if field == "cid" {
		return "cid"
	}
	return "pid"
}

func categoryStableKey(id int64) string {
	return strings.TrimSpace(support.GetCategoryStableKeyByID(support.ResolveCategoryID(id)))
}

func currentCategoryIDsBySourceKey(sourceKey string) []int64 {
	sourceKey = strings.TrimSpace(sourceKey)
	if sourceKey == "" {
		return nil
	}
	var mappings []model.CategoryMapping
	if err := db.Mdb.Find(&mappings).Error; err != nil {
		return nil
	}
	ids := make([]int64, 0)
	seen := make(map[int64]struct{})
	for _, mapping := range mappings {
		if support.BuildSourceCategoryKey(mapping.SourceId, mapping.SourceTypeId) != sourceKey {
			continue
		}
		if mapping.CategoryId <= 0 {
			continue
		}
		if _, ok := seen[mapping.CategoryId]; ok {
			continue
		}
		seen[mapping.CategoryId] = struct{}{}
		ids = append(ids, mapping.CategoryId)
	}
	return ids
}

func currentRootCategoryIDsForSearch(search model.FilmIndex) []int64 {
	ids := currentCategoryIDsBySourceKey(search.RootCategoryKey)
	if len(ids) == 0 && search.Pid > 0 {
		ids = []int64{support.ResolveCategoryID(search.Pid)}
	}
	roots := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		rootID := support.GetRootId(id)
		if rootID <= 0 {
			rootID = id
		}
		if rootID <= 0 {
			continue
		}
		if _, ok := seen[rootID]; ok {
			continue
		}
		seen[rootID] = struct{}{}
		roots = append(roots, rootID)
	}
	return roots
}

func currentCategoryIDsForSearch(search model.FilmIndex) []int64 {
	ids := currentCategoryIDsBySourceKey(search.CategoryKey)
	if len(ids) == 0 && search.Cid > 0 {
		ids = []int64{support.ResolveCategoryID(search.Cid)}
	}
	return ids
}

func applySameCurrentRootCategoryFilter(query *gorm.DB, search model.FilmIndex) *gorm.DB {
	rootIDs := currentRootCategoryIDsForSearch(search)
	if len(rootIDs) == 0 {
		return query.Where("1 = 0")
	}
	rootQuery := db.Mdb.Where("1 = 0")
	for _, rootID := range rootIDs {
		rootQuery = rootQuery.Or(applyCategoryFieldFilter(db.Mdb.Model(&model.FilmIndex{}), "pid", rootID))
	}
	return query.Where(rootQuery)
}

func applySameCurrentCategoryFilter(query *gorm.DB, search model.FilmIndex) *gorm.DB {
	categoryIDs := currentCategoryIDsForSearch(search)
	if len(categoryIDs) == 0 {
		return query.Where("1 = 0")
	}
	categoryQuery := db.Mdb.Where("1 = 0")
	for _, categoryID := range categoryIDs {
		categoryQuery = categoryQuery.Or(applyCategoryFieldFilter(db.Mdb.Model(&model.FilmIndex{}), "cid", categoryID))
	}
	return query.Where(categoryQuery)
}

func sourceCategoryKeysByCategoryIDs(categoryIDs []int64) []string {
	if len(categoryIDs) == 0 {
		return nil
	}
	var mappings []model.CategoryMapping
	if err := db.Mdb.Where("category_id IN ?", categoryIDs).Find(&mappings).Error; err != nil {
		return nil
	}
	keys := make([]string, 0, len(mappings))
	seen := make(map[string]struct{}, len(mappings))
	for _, mapping := range mappings {
		key := support.BuildSourceCategoryKey(mapping.SourceId, mapping.SourceTypeId)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

func visibleDescendantCategoryIDs(categoryID int64) []int64 {
	if categoryID <= 0 {
		return nil
	}
	ids := make([]int64, 0)
	queue := []int64{categoryID}
	seen := map[int64]struct{}{categoryID: {}}
	for len(queue) > 0 {
		parentID := queue[0]
		queue = queue[1:]

		var children []model.Category
		db.Mdb.Where("pid = ? AND `show` = ?", parentID, true).Order("sort ASC, id ASC").Find(&children)
		for _, child := range children {
			if child.Id <= 0 {
				continue
			}
			if _, ok := seen[child.Id]; ok {
				continue
			}
			seen[child.Id] = struct{}{}
			ids = append(ids, child.Id)
			queue = append(queue, child.Id)
		}
	}
	return ids
}

func visibleCategoryAndDescendantIDs(categoryID int64) []int64 {
	categoryID = support.ResolveCategoryID(categoryID)
	if categoryID <= 0 {
		return nil
	}
	ids := []int64{categoryID}
	ids = append(ids, visibleDescendantCategoryIDs(categoryID)...)
	return ids
}

func categorySourceKeys(field string, id int64) []string {
	category, ok := categoryByID(id)
	if !ok {
		return nil
	}
	if field == "cid" {
		return sourceCategoryKeysByCategoryIDs(visibleCategoryAndDescendantIDs(category.Id))
	}

	rootID := category.Id
	if category.Pid > 0 {
		rootID = support.GetRootId(category.Id)
	}
	return sourceCategoryKeysByCategoryIDs(visibleCategoryAndDescendantIDs(rootID))
}

func rootCategorySourceKeyGroups(id int64) ([]string, []string) {
	category, ok := categoryByID(id)
	if !ok {
		return nil, nil
	}
	rootID := category.Id
	if category.Pid > 0 {
		rootID = support.GetRootId(category.Id)
	}
	visibleCategoryIDs := visibleCategoryAndDescendantIDs(rootID)
	return sourceCategoryKeysByCategoryIDs([]int64{rootID}), sourceCategoryKeysByCategoryIDs(visibleCategoryIDs)
}

func applyRootCategorySourceFilter(query *gorm.DB, rootID int64) *gorm.DB {
	rootKeys, visibleKeys := rootCategorySourceKeyGroups(rootID)
	if len(visibleKeys) == 0 {
		return query
	}
	visibleCategoryQuery := db.Mdb.Where("category_key IN ?", visibleKeys)
	if len(rootKeys) > 0 {
		visibleCategoryQuery = visibleCategoryQuery.Or("root_category_key IN ? AND (category_key = '' OR category_key IS NULL)", rootKeys)
	}
	return query.Where(visibleCategoryQuery)
}

func visibleCategoryGroups() ([]int64, []int64) {
	var categories []model.Category
	if err := db.Mdb.Where("`show` = ?", true).Find(&categories).Error; err != nil {
		return nil, nil
	}

	rootSet := make(map[int64]struct{}, len(categories))
	for _, category := range categories {
		if category.Pid == 0 {
			rootSet[category.Id] = struct{}{}
		}
	}

	rootIDs := make([]int64, 0, len(rootSet))
	categoryIDs := make([]int64, 0, len(categories))
	for _, category := range categories {
		if category.Pid == 0 {
			rootIDs = append(rootIDs, category.Id)
			categoryIDs = append(categoryIDs, category.Id)
			continue
		}
		if _, ok := rootSet[category.Pid]; ok {
			categoryIDs = append(categoryIDs, category.Id)
		}
	}
	return rootIDs, categoryIDs
}

func stableKeysByCategoryIDs(categoryIDs []int64) []string {
	if len(categoryIDs) == 0 {
		return nil
	}
	var keys []string
	db.Mdb.Model(&model.Category{}).
		Where("id IN ? AND stable_key <> ''", categoryIDs).
		Pluck("stable_key", &keys)
	return slices.Compact(keys)
}

func applyVisibleCategoryFilter(query *gorm.DB) *gorm.DB {
	rootIDs, categoryIDs := visibleCategoryGroups()
	if len(categoryIDs) == 0 {
		return emptyFilmIndexQuery(query)
	}

	rootKeys := sourceCategoryKeysByCategoryIDs(rootIDs)
	visibleKeys := sourceCategoryKeysByCategoryIDs(categoryIDs)
	if len(visibleKeys) == 0 {
		visibleKeys = stableKeysByCategoryIDs(categoryIDs)
		rootKeys = stableKeysByCategoryIDs(rootIDs)
	}
	if len(visibleKeys) > 0 {
		visibleQuery := db.Mdb.Where("category_key IN ?", visibleKeys)
		if len(rootKeys) > 0 {
			visibleQuery = visibleQuery.Or("root_category_key IN ? AND (category_key = '' OR category_key IS NULL)", rootKeys)
		}
		return query.Where(visibleQuery)
	}
	return query.Where("cid IN ? OR (pid IN ? AND cid = 0)", categoryIDs, rootIDs)
}

func categoryByID(id int64) (*model.Category, bool) {
	resolvedID := support.ResolveCategoryID(id)
	if resolvedID <= 0 {
		return nil, false
	}
	var category model.Category
	if err := db.Mdb.Where("id = ?", resolvedID).First(&category).Error; err != nil {
		return nil, false
	}
	return &category, true
}

func emptyFilmIndexQuery(query *gorm.DB) *gorm.DB {
	return query.Where("1 = 0")
}

func applyCategoryVisibilityFilter(query *gorm.DB, field string, id int64) *gorm.DB {
	category, ok := categoryByID(id)
	if !ok || !category.Show {
		return emptyFilmIndexQuery(query)
	}

	if field == "pid" {
		rootID := category.Id
		if category.Pid > 0 {
			rootID = support.GetRootId(category.Id)
		}
		if root, ok := categoryByID(rootID); !ok || !root.Show {
			return emptyFilmIndexQuery(query)
		}
		return query
	}

	if category.Pid > 0 {
		if parent, ok := categoryByID(category.Pid); !ok || !parent.Show {
			return emptyFilmIndexQuery(query)
		}
	}
	return query
}

func applyCategoryFieldFilter(query *gorm.DB, field string, id int64) *gorm.DB {
	resolvedID := support.ResolveCategoryID(id)
	if resolvedID <= 0 {
		return emptyFilmIndexQuery(query)
	}
	query = applyCategoryVisibilityFilter(query, field, resolvedID)
	if keys := categorySourceKeys(field, resolvedID); len(keys) > 0 {
		if field == "pid" {
			return applyRootCategorySourceFilter(query, resolvedID)
		}
		return query.Where("category_key IN ?", keys)
	}
	if stableKey := categoryStableKey(resolvedID); stableKey != "" {
		return query.Where(fmt.Sprintf("%s = ?", categoryKeyColumn(field)), stableKey)
	}
	return query.Where(fmt.Sprintf("%s = ?", categoryIDColumn(field)), resolvedID)
}

func ApplyCategoryFilter(query *gorm.DB, pid int64, cid int64) *gorm.DB {
	isUncategorized := cid == model.TagUncategorizedValue
	pid = support.ResolveCategoryID(pid)
	if cid > 0 {
		cid = support.ResolveCategoryID(cid)
	}
	switch {
	case isUncategorized && pid > 0:
		if rootKeys, _ := rootCategorySourceKeyGroups(pid); len(rootKeys) > 0 {
			return query.Where("root_category_key IN ? AND (category_key = '' OR category_key IS NULL)", rootKeys)
		}
		if rootKey := categoryStableKey(pid); rootKey != "" {
			return query.Where("root_category_key = ? AND (category_key = '' OR category_key IS NULL)", rootKey)
		}
		return query.Where("pid = ? AND cid = 0", pid)
	case cid > 0 && support.IsRootCategory(cid):
		return applyCategoryFieldFilter(query, "pid", cid)
	case cid > 0:
		return applyCategoryFieldFilter(query, "cid", cid)
	case pid > 0:
		return applyCategoryFieldFilter(query, "pid", pid)
	default:
		return applyVisibleCategoryFilter(query)
	}
}

func applyOriginalCategoryFilter(query *gorm.DB, pid int64, value string) *gorm.DB {
	pid = support.ResolveCategoryID(pid)
	value = strings.TrimSpace(value)
	if pid <= 0 || value == "" {
		return query
	}
	query = applyCategoryFieldFilter(query, "pid", pid)
	return query.Where("original_category = ?", value)
}

func GetOriginalCategoryOptions(pid int64) []string {
	pid = support.ResolveCategoryID(pid)
	if pid <= 0 {
		return nil
	}

	var values []string
	query := applyCategoryFieldFilter(db.Mdb.Model(&model.FilmIndex{}), "pid", pid)
	if err := query.
		Distinct("original_category").
		Where("original_category <> '' AND original_category IS NOT NULL").
		Order("original_category ASC").
		Pluck("original_category", &values).Error; err != nil {
		log.Printf("GetOriginalCategoryOptions Error: %v", err)
		return nil
	}
	values = slices.Compact(values)
	if len(values) <= 1 {
		return nil
	}
	return values
}

func buildCategoryQuery(field string, id int64) *gorm.DB {
	return applyCategoryFieldFilter(db.Mdb.Model(&model.FilmIndex{}), field, id)
}

func applyPageStats(query *gorm.DB, page *dto.Page) *dto.Page {
	page = ensurePage(page)
	dto.GetPage(query, page)
	return page
}

func queryMovieListByCategory(field string, id int64, limit int, offset int) []model.MovieBasicInfo {
	var filmIndexes []model.FilmIndex
	if err := buildCategoryQuery(field, id).
		Order(latestUpdateOrderSQL).
		Limit(limit).
		Offset(offset).
		Find(&filmIndexes).Error; err != nil {
		log.Printf("queryMovieListByCategory Error: %v", err)
		return nil
	}
	return BuildMovieBasicInfos(filmIndexes...)
}

func queryHotMoviesByCategory(field string, id int64, limit int, offset int) []model.FilmIndex {
	var filmIndexes []model.FilmIndex
	hotSince := time.Now().AddDate(0, -1, 0).Unix()
	if err := buildCategoryQuery(field, id).
		Where("update_stamp > ?", hotSince).
		Order("year DESC, hits DESC, mid DESC").
		Limit(limit).
		Offset(offset).
		Find(&filmIndexes).Error; err != nil {
		log.Printf("queryHotMoviesByCategory Error: %v", err)
		return nil
	}
	return filmIndexes
}

func applyMovieSortQuery(query *gorm.DB, sortType int) *gorm.DB {
	switch sortType {
	case 0:
		return query.Order("year DESC, " + latestUpdateOrderSQL)
	case 1:
		return query.Order("hits DESC, mid DESC")
	case 2:
		return query.Order(latestUpdateOrderSQL)
	default:
		return query.Order(latestUpdateOrderSQL)
	}
}

func queryMovieListBySort(pid int64, sortType int, limit int, offset int) []model.MovieBasicInfo {
	var filmIndexes []model.FilmIndex
	query := applyMovieSortQuery(buildCategoryQuery("pid", pid), sortType)
	if err := query.Limit(limit).Offset(offset).Find(&filmIndexes).Error; err != nil {
		log.Printf("queryMovieListBySort Error: %v", err)
		return nil
	}
	return BuildMovieBasicInfos(filmIndexes...)
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

func SearchFilmKeyword(keyword string, page *dto.Page) []model.FilmIndex {
	page = ensurePage(page)
	keywordQuery := buildNameKeywordQuery(keyword)
	var filmIndexes []model.FilmIndex
	query := applyVisibleCategoryFilter(db.Mdb.Model(&model.FilmIndex{})).
		Where(keywordQuery).
		Order("year DESC, " + latestUpdateOrderSQL)

	dto.GetPage(query, page)
	query.Limit(page.PageSize).Offset(getPageOffset(page)).Find(&filmIndexes)

	return filmIndexes
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
	Movie model.FilmIndex
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

func appendUniqueRelatedCandidates(dst []model.FilmIndex, src []model.FilmIndex, seen map[int64]struct{}, limit int) []model.FilmIndex {
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

func queryRelatedCandidates(search model.FilmIndex, limit int, apply func(query *gorm.DB) *gorm.DB) []model.FilmIndex {
	if limit <= 0 {
		return nil
	}
	query := db.Mdb.Model(&model.FilmIndex{}).
		Where("mid != ?", search.Mid).
		Where("deleted_at IS NULL")
	query = applySameCurrentRootCategoryFilter(query, search)
	if apply != nil {
		query = apply(query)
	}
	var list []model.FilmIndex
	if err := query.Order(latestUpdateOrderSQL).Limit(limit).Find(&list).Error; err != nil {
		log.Printf("queryRelatedCandidates Error: %v", err)
		return nil
	}
	return list
}

func loadRelatedCandidates(search model.FilmIndex, limit int) []model.FilmIndex {
	coreToken := extractCoreSearchToken(search.Name)
	tags := splitClassTags(search.ClassTag)
	list := make([]model.FilmIndex, 0, limit)
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
			return applySameCurrentCategoryFilter(query, search)
		}), seen, limit)
	}
	for _, tag := range tags {
		list = appendUniqueRelatedCandidates(list, queryRelatedCandidates(search, tagLimit, func(query *gorm.DB) *gorm.DB {
			return query.Where("class_tag LIKE ?", fmt.Sprintf("%%%s%%", tag))
		}), seen, limit)
	}
	return list
}

func calcTitleScore(coreToken string, candidate model.FilmIndex) int {
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

func calcAliasScore(current model.FilmIndex, candidate model.FilmIndex) int {
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

func calcMetaScore(current, candidate model.FilmIndex) int {
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

func freshnessBoost(candidate model.FilmIndex) int {
	stamp := candidate.UpdateStamp
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

func scoreRelatedCandidate(current model.FilmIndex, candidate model.FilmIndex) relatedCandidateScore {
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

func rankRelatedCandidates(current model.FilmIndex, candidates []model.FilmIndex, pageSize int) []model.FilmIndex {
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
	list := make([]model.FilmIndex, 0, len(scored))
	for _, item := range scored {
		list = append(list, item.Movie)
	}
	return list
}

func loadFallbackCandidates(search model.FilmIndex, limit int, exclude map[int64]struct{}) []model.FilmIndex {
	if limit <= 0 {
		return nil
	}
	appendUnique := func(dst []model.FilmIndex, source []model.FilmIndex, max int) []model.FilmIndex {
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
	var result []model.FilmIndex
	if search.Cid > 0 {
		result = appendUnique(result, getFallbackRelatedSearchInfos(search, &dto.Page{Current: 1, PageSize: limit}), limit)
	}
	if len(result) >= limit || search.Pid <= 0 {
		return result
	}
	var pidHotList []model.FilmIndex
	hotSince := time.Now().AddDate(0, -1, 0).Unix()
	pidHotQuery := db.Mdb.Model(&model.FilmIndex{}).
		Where("mid != ?", search.Mid).
		Where("deleted_at IS NULL").
		Where("update_stamp > ?", hotSince)
	pidHotQuery = applySameCurrentRootCategoryFilter(pidHotQuery, search)
	if err := pidHotQuery.
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
	var pidList []model.FilmIndex
	pidQuery := db.Mdb.Model(&model.FilmIndex{}).
		Where("mid != ?", search.Mid).
		Where("deleted_at IS NULL")
	pidQuery = applySameCurrentRootCategoryFilter(pidQuery, search)
	if err := pidQuery.
		Order(latestUpdateOrderSQL).
		Limit(limit * 2).
		Find(&pidList).Error; err != nil {
		log.Printf("loadFallbackCandidates Pid Fallback Error: %v", err)
		return result
	}
	return appendUnique(result, pidList, limit)
}

func buildRelatedMovieQuery(search model.FilmIndex, coreToken string, tags []string) *gorm.DB {
	nameLike := fmt.Sprintf("%%%s%%", coreToken)
	prefixLike := fmt.Sprintf("%s%%", coreToken)
	escapedCoreToken := strings.ReplaceAll(coreToken, "'", "''")
	escapedPrefixLike := strings.ReplaceAll(prefixLike, "'", "''")
	escapedNameLike := strings.ReplaceAll(nameLike, "'", "''")

	query := db.Mdb.Model(&model.FilmIndex{}).
		Where("mid != ?", search.Mid).
		Where("deleted_at IS NULL")
	query = applySameCurrentRootCategoryFilter(query, search)

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

func getFallbackRelatedSearchInfos(search model.FilmIndex, page *dto.Page) []model.FilmIndex {
	if search.Cid <= 0 {
		return nil
	}

	var list []model.FilmIndex
	query := db.Mdb.Model(&model.FilmIndex{}).
		Where("mid != ?", search.Mid).
		Where("deleted_at IS NULL")
	query = applySameCurrentCategoryFilter(query, search)
	if err := query.
		Order(latestUpdateOrderSQL).
		Offset(getPageOffset(page)).
		Limit(page.PageSize).
		Find(&list).Error; err != nil {
		log.Printf("GetRelateMovieBasicInfo Fallback Error: %v", err)
		return nil
	}
	return list
}

func GetRelateMovieBasicInfo(search model.FilmIndex, page *dto.Page) []model.MovieBasicInfo {
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
	return BuildMovieBasicInfos(ranked[offset:end]...)
}

// GetBasicInfoByKey 获取影片的基本信息
func GetBasicInfoByKey(cid int64, mid int64) model.MovieBasicInfo {
	index := GetFilmIndexById(mid)
	if index != nil {
		return BuildMovieBasicInfos(*index)[0]
	}
	return model.MovieBasicInfo{}
}

// GetMovieDetail 获取影片详情信息
func GetMovieDetail(cid int64, mid int64) *model.MovieDetail {
	index := GetFilmIndexById(mid)
	if index == nil {
		return nil
	}

	var movieDetailInfo model.MovieDetailInfo
	if err := db.Mdb.Where("mid = ?", mid).First(&movieDetailInfo).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("GetMovieDetail Error: %v", err)
		}
		return nil
	}
	var detail model.MovieDetail
	if err := json.Unmarshal([]byte(movieDetailInfo.Content), &detail); err != nil {
		log.Printf("Unmarshal MovieDetail Error: %v", err)
		return nil
	}
	ApplyFilmIndex(&detail, *index)

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

func GetSearchPage(s model.SearchVo) []model.FilmIndex {
	page := ensurePage(s.Paging)

	query := applySearchPageFilters(db.Mdb.Model(&model.FilmIndex{}), s).Order(latestUpdateOrderSQL)

	dto.GetPage(query, page)
	var sl []model.FilmIndex
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
		othersQuery := db.Mdb.Where("year <= 0 OR year IS NULL")
		if len(topVals) > 0 {
			othersQuery = othersQuery.Or("year > 0 AND year NOT IN ?", topVals)
		}
		return query.Where(othersQuery)
	case model.TagUnknownValue:
		return query.Where("year <= 0 OR year IS NULL")
	default:
		return query.Where("year = ?", value)
	}
}

func applyTextTagFilter(query *gorm.DB, pid int64, fieldName string, column string, value string) *gorm.DB {
	switch value {
	case model.TagOthersValue:
		topVals := GetTopTagValues(pid, fieldName)
		othersQuery := db.Mdb.Where(hasTextValue(column))
		if len(topVals) > 0 {
			othersQuery = othersQuery.Where(fmt.Sprintf("%s NOT IN ?", column), topVals)
		}
		for _, abnormal := range getAbnormalSearchTagValues(pid, fieldName) {
			othersQuery = othersQuery.Or(fmt.Sprintf("%s = ?", column), abnormal)
		}
		return query.Where(othersQuery)
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
		othersQuery := db.Mdb.Where(hasTextValue("class_tag"))
		excludeCount := len(topVals)
		if excludeCount > maxPlotExcludes {
			excludeCount = maxPlotExcludes
		}
		for i := 0; i < excludeCount; i++ {
			othersQuery = othersQuery.Where("class_tag NOT LIKE ?", fmt.Sprintf("%%%v%%", topVals[i]))
		}
		for _, abnormal := range getAbnormalSearchTagValues(pid, fieldName) {
			othersQuery = othersQuery.Or("class_tag LIKE ?", fmt.Sprintf("%%%v%%", abnormal))
		}
		return query.Where(othersQuery)
	case model.TagUnknownValue:
		return query.Where(isUnknownTextValue("class_tag"))
	default:
		return query.Where("class_tag LIKE ?", fmt.Sprintf("%%%v%%", value))
	}
}

func applySearchTagSort(query *gorm.DB, value string) *gorm.DB {
	if value == "" {
		value = "update_stamp"
	}
	column, allowed := allowedSearchSortColumns[value]
	if !allowed {
		column = allowedSearchSortColumns["update_stamp"]
	}
	if strings.EqualFold(column, "update_stamp") {
		return query.Order(latestUpdateOrderSQL)
	}
	if strings.EqualFold(column, "year") {
		return query.Order("year DESC, " + latestUpdateOrderSQL)
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

func BuildFilmIndexQueryByTags(query *gorm.DB, st model.SearchTagsVO) *gorm.DB {
	st = normalizeSearchTagsVO(st)
	return applySearchTagFilters(query, st)
}

func ListFilmIndexesByTags(st model.SearchTagsVO, page *dto.Page) []model.FilmIndex {
	page = ensurePage(page)
	qw := BuildFilmIndexQueryByTags(db.Mdb.Model(&model.FilmIndex{}), st)

	dto.GetPage(qw, page)
	var filmIndexes []model.FilmIndex
	if err := qw.Limit(page.PageSize).Offset((page.Current - 1) * page.PageSize).Find(&filmIndexes).Error; err != nil {
		log.Printf("ListFilmIndexesByTags Error: %v", err)
		return nil
	}
	return filmIndexes
}

func GetFilmIndexById(id int64) *model.FilmIndex {
	s := model.FilmIndex{}
	if err := db.Mdb.Where("mid = ?", id).First(&s).Error; err != nil {
		return nil
	}
	return &s
}

// GetHotMovieByPid 获取当前级分类下的热门影片
func GetHotMovieByPid(pid int64, page *dto.Page) []model.FilmIndex {
	page = ensurePage(page)
	return GetHotMovieByPidLimit(pid, page.PageSize, getPageOffset(page))
}

// GetHotMovieByPidLimit 轻量级获取热门影片
func GetHotMovieByPidLimit(pid int64, limit, offset int) []model.FilmIndex {
	return queryHotMoviesByCategory("pid", pid, limit, offset)
}

// GetHotMovieByCid 获取当前分类下的热门影片
func GetHotMovieByCid(cid int64, page *dto.Page) []model.FilmIndex {
	page = ensurePage(page)
	return GetHotMovieByCidLimit(cid, page.PageSize, getPageOffset(page))
}

// GetHotMovieByCidLimit 轻量级获取热门影片
func GetHotMovieByCidLimit(cid int64, limit, offset int) []model.FilmIndex {
	return queryHotMoviesByCategory("cid", cid, limit, offset)
}

// GetMovieListBySort 通过排序类型返回对应的影片基本信息
func GetMovieListBySort(t int, pid int64, page *dto.Page) []model.MovieBasicInfo {
	page = ensurePage(page)
	return queryMovieListBySort(pid, t, page.PageSize, getPageOffset(page))
}
