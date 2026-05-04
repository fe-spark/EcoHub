package film

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

func hasEffectiveSearchOptions(options []map[string]string) bool {
	for _, option := range options {
		if strings.TrimSpace(option["Value"]) != "" {
			return true
		}
	}
	return false
}

func buildCategorySearchOptions(pid int64, sticky string) []map[string]string {
	pid = support.ResolveCategoryID(pid)
	formatted := HandleTagStr("Category", true)
	if pid <= 0 {
		return formatted
	}

	var categories []model.Category
	db.Mdb.Where("pid = ? AND `show` = ?", pid, true).Order("sort ASC, id ASC").Find(&categories)
	for _, category := range categories {
		formatted = append(formatted, map[string]string{
			"Name":  category.Name,
			"Value": fmt.Sprint(category.Id),
		})
	}

	if sticky != "" {
		stickyValue := strings.TrimSpace(sticky)
		exists := false
		for _, item := range formatted {
			if item["Value"] == stickyValue {
				exists = true
				break
			}
		}
		if !exists {
			var category model.Category
			if err := db.Mdb.Where("id = ? AND pid = ? AND `show` = ?", stickyValue, pid, true).First(&category).Error; err == nil {
				formatted = append(formatted, map[string]string{
					"Name":  category.Name,
					"Value": fmt.Sprint(category.Id),
				})
			}
		}
	}

	return formatted
}

func GetTagsByTitle(pid int64, tagType string) []map[string]string {
	pid = support.ResolveCategoryID(pid)
	var tags []string
	var items []model.SearchTagItem

	db.Mdb.Where("pid = ? AND tag_type = ? AND score > 5", pid, tagType).
		Order("score DESC, value ASC, id ASC").Limit(30).Find(&items)

	for _, item := range items {
		tags = append(tags, fmt.Sprintf("%s:%s", item.Name, item.Value))
	}

	if len(tags) == 0 && tagType == "Sort" {
		tags = defaultSortTagStrings
	}
	return HandleTagStr(tagType, true, tags...)
}

func GetTopTagValues(pid int64, tagType string) []string {
	pid = support.ResolveCategoryID(pid)
	if strings.EqualFold(tagType, "Year") {
		items := loadSearchTagItemsByType(model.SearchTagsVO{Pid: pid})[tagType]
		items = SortYearSearchTagItems(items)
		items = LimitSearchTagItems(items, SearchTagDisplayLimit)

		vals := make([]string, 0, len(items))
		for _, item := range items {
			vals = append(vals, item.Value)
		}
		return vals
	}

	var vals []string
	for _, item := range LimitSearchTagItems(loadSearchTagItemsByType(model.SearchTagsVO{Pid: pid})[tagType], SearchTagDisplayLimit) {
		vals = append(vals, item.Value)
	}
	return vals
}

func shouldAlwaysExposeSearchTag(tagType string) bool {
	switch tagType {
	case "Plot", "Area", "Language", "Year":
		return true
	default:
		return false
	}
}

func buildSearchTagCacheKey(st model.SearchTagsVO, snapshotVersion string) string {
	st = normalizeSearchTagsVO(st)
	return fmt.Sprintf("%s:v%s:s%s:%d",
		config.SearchTags,
		getSearchTagsCacheVersion(),
		snapshotVersion,
		st.Pid,
	)
}

type searchTagFacts struct {
	ItemsByType map[string][]model.SearchTagItem `json:"itemsByType"`
	HasOthers   map[string]bool                  `json:"hasOthers"`
}

func normalizeSearchTagsVO(st model.SearchTagsVO) model.SearchTagsVO {
	st.Pid = support.ResolveCategoryID(st.Pid)
	if st.Cid > 0 {
		st.Cid = support.ResolveCategoryID(st.Cid)
	}
	return st
}

func baseSearchTagFactQuery(st model.SearchTagsVO, snapshotVersion string) *gorm.DB {
	st = normalizeSearchTagsVO(st)
	query := db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", strings.TrimSpace(snapshotVersion))
	// 筛选项不联动：选项统计固定在当前一级分类内；列表查询仍在 BuildFilmIndexQueryByTags 中完整应用所有筛选条件。
	return ApplyCategoryFilter(query, st.Pid, 0)
}

func searchTagItemsByColumn(st model.SearchTagsVO, tagType string, column string) []model.SearchTagItem {
	return searchTagItemsByColumnFromQuery(baseSearchTagFactQuery(st, GetActiveSnapshotVersion()), tagType, column)
}

func searchTagItemsByColumnFromQuery(query *gorm.DB, tagType string, column string) []model.SearchTagItem {
	type tagCount struct {
		Value string
		Score int64
	}

	var rows []tagCount
	if err := query.
		Select(fmt.Sprintf("%s AS value, COUNT(*) AS score", column)).
		Where(hasTextValue(column)).
		Group(column).
		Order("score DESC, value ASC").
		Scan(&rows).Error; err != nil {
		return nil
	}

	items := make([]model.SearchTagItem, 0, len(rows))
	for _, row := range rows {
		value := normalizeSearchTagValue(tagType, row.Value)
		if value == "" {
			continue
		}
		items = append(items, model.SearchTagItem{TagType: tagType, Name: value, Value: value, Score: row.Score})
	}
	return items
}

func searchYearTagItems(st model.SearchTagsVO) []model.SearchTagItem {
	return searchYearTagItemsFromQuery(baseSearchTagFactQuery(st, GetActiveSnapshotVersion()))
}

func searchYearTagItemsFromQuery(query *gorm.DB) []model.SearchTagItem {
	type yearCount struct {
		Value int64
		Score int64
	}

	var rows []yearCount
	if err := query.
		Select("year AS value, COUNT(*) AS score").
		Where("year > 0").
		Group("year").
		Order("year DESC").
		Scan(&rows).Error; err != nil {
		return nil
	}

	items := make([]model.SearchTagItem, 0, len(rows))
	for _, row := range rows {
		value := strconv.FormatInt(row.Value, 10)
		items = append(items, model.SearchTagItem{TagType: "Year", Name: value, Value: value, Score: row.Score})
	}
	return items
}

func searchPlotTagItems(st model.SearchTagsVO) []model.SearchTagItem {
	return searchPlotTagItemsFromQuery(baseSearchTagFactQuery(st, GetActiveSnapshotVersion()))
}

func searchPlotTagItemsFromQuery(query *gorm.DB) []model.SearchTagItem {
	var classTags []string
	if err := query.
		Where(hasTextValue("class_tag")).
		Pluck("class_tag", &classTags).Error; err != nil {
		return nil
	}

	counts := make(map[string]int64)
	for _, classTag := range classTags {
		for _, part := range reTagSplit.Split(classTag, -1) {
			value := normalizeSearchTagValue("Plot", part)
			if value == "" || value == model.TagOthersValue || value == "其他" || value == "其它" || value == "全部" || value == "剧情" || value == "暂无" {
				continue
			}
			counts[value]++
		}
	}

	items := make([]model.SearchTagItem, 0, len(counts))
	for value, score := range counts {
		items = append(items, model.SearchTagItem{TagType: "Plot", Name: value, Value: value, Score: score})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score == items[j].Score {
			return items[i].Value < items[j].Value
		}
		return items[i].Score > items[j].Score
	})
	return items
}

func loadSearchTagItemsByType(st model.SearchTagsVO) map[string][]model.SearchTagItem {
	return loadSearchTagItemsByTypeForVersion(st, GetActiveSnapshotVersion())

}

func loadSearchTagItemsByTypeForVersion(st model.SearchTagsVO, snapshotVersion string) map[string][]model.SearchTagItem {
	return loadSearchTagItemsByTypeFromQuery(st, baseSearchTagFactQuery(st, snapshotVersion))
}

func loadSearchTagItemsByTypeFromQuery(st model.SearchTagsVO, query *gorm.DB) map[string][]model.SearchTagItem {
	st = normalizeSearchTagsVO(st)
	itemsByType := make(map[string][]model.SearchTagItem)
	if st.Pid <= 0 {
		return itemsByType
	}

	itemsByType["Area"] = searchTagItemsByColumnFromQuery(query.Session(&gorm.Session{}), "Area", "area")
	itemsByType["Language"] = searchTagItemsByColumnFromQuery(query.Session(&gorm.Session{}), "Language", "language")
	itemsByType["Year"] = searchYearTagItemsFromQuery(query.Session(&gorm.Session{}))
	itemsByType["Plot"] = searchPlotTagItemsFromQuery(query.Session(&gorm.Session{}))
	return itemsByType
}

func loadLegacySearchTagItemsByType(pid int64) map[string][]model.SearchTagItem {
	pid = support.ResolveCategoryID(pid)
	var allItems []model.SearchTagItem
	db.Mdb.Where("pid = ? AND score > 0", pid).Order("tag_type ASC, score DESC, value ASC, id ASC").Find(&allItems)

	itemsByType := make(map[string][]model.SearchTagItem)
	for _, item := range allItems {
		itemsByType[item.TagType] = append(itemsByType[item.TagType], item)
	}
	return itemsByType
}

func getAbnormalSearchTagValues(pid int64, tagType string) []string {
	items := loadSearchTagItemsByType(model.SearchTagsVO{Pid: pid})[tagType]
	if len(items) == 0 {
		items = loadLegacySearchTagItemsByType(pid)[tagType]
	}
	_, abnormalItems := SplitSearchTagItems(tagType, items)
	values := make([]string, 0, len(abnormalItems))
	seen := make(map[string]struct{}, len(abnormalItems))
	for _, item := range abnormalItems {
		value := strings.TrimSpace(item.Value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

func hasOthersSearchFacts(pid int64, tagType string, snapshotVersion string) bool {
	pid = support.ResolveCategoryID(pid)
	snapshotVersion = strings.TrimSpace(snapshotVersion)
	if pid <= 0 || snapshotVersion == "" {
		return false
	}

	query := applyCategoryFieldFilter(db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", snapshotVersion), "pid", pid)
	switch tagType {
	case "Year":
		query = query.Where("year <= 0 OR year IS NULL")
	case "Area":
		query = query.Where(isUnknownTextValue("area"))
	case "Language":
		query = query.Where(isUnknownTextValue("language"))
	case "Plot":
		query = query.Where(isUnknownTextValue("class_tag"))
	default:
		return false
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

func getStickySearchTagValue(st model.SearchTagsVO, tagType string) string {
	switch tagType {
	case "Category":
		return fmt.Sprint(st.Cid)
	case "OriginalCategory":
		return st.OriginalCategory
	case "Plot":
		return st.Plot
	case "Area":
		return st.Area
	case "Language":
		return st.Language
	case "Year":
		return st.Year
	default:
		return ""
	}
}

func buildOriginalCategorySearchOptions(pid int64, sticky string) []map[string]string {
	formatted := HandleTagStr("OriginalCategory", true)
	pid = support.ResolveCategoryID(pid)
	if pid <= 0 {
		return formatted
	}

	values := GetOriginalCategoryOptions(pid)
	for _, value := range values {
		formatted = append(formatted, map[string]string{
			"Name":  value,
			"Value": value,
		})
	}

	if strings.TrimSpace(sticky) != "" {
		formatted = AppendSearchOption(formatted, map[string]string{
			"Name":  sticky,
			"Value": sticky,
		})
	}

	return formatted
}

// GetSearchTag 获取搜索标签 (带联动感知与复合 Redis 缓存)
func GetSearchTag(st model.SearchTagsVO) map[string]any {
	st = normalizeSearchTagsVO(st)
	pid := st.Pid
	snapshotVersion := GetActiveSnapshotVersion()
	cacheKey := ""

	if snapshotVersion != "" {
		cacheKey = buildSearchTagCacheKey(st, snapshotVersion)
	}

	if cacheKey != "" {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var facts searchTagFacts
			if json.Unmarshal([]byte(data), &facts) == nil {
				return buildSearchTagResponse(st, snapshotVersion, facts)
			}
		}
	}

	facts := searchTagFacts{
		ItemsByType: make(map[string][]model.SearchTagItem),
		HasOthers:   make(map[string]bool),
	}
	if snapshotVersion != "" {
		facts.ItemsByType = loadSearchTagItemsByTypeForVersion(st, snapshotVersion)
	}
	for _, t := range []string{"Plot", "Area", "Language", "Year"} {
		facts.HasOthers[t] = hasOthersSearchFacts(pid, t, snapshotVersion)
	}

	if cacheKey != "" {
		if data, err := json.Marshal(facts); err == nil {
			db.Rdb.Set(db.Cxt, cacheKey, string(data), time.Hour*2)
		}
	}

	return buildSearchTagResponse(st, snapshotVersion, facts)
}

func buildSearchTagResponse(st model.SearchTagsVO, snapshotVersion string, facts searchTagFacts) map[string]any {
	st = normalizeSearchTagsVO(st)
	pid := st.Pid
	res := make(map[string]any)
	tagTypes := []string{"Category", "Plot", "Area", "Language", "Year", "Sort"}
	allTitles := map[string]string{
		"Category": "类型",
		"Plot":     "剧情",
		"Area":     "地区",
		"Language": "语言",
		"Year":     "年份",
		"Sort":     "排序",
	}

	tagMap := make(map[string]any)
	activeTitles := make(map[string]string)
	activeSortList := make([]string, 0)
	itemsByType := facts.ItemsByType
	if itemsByType == nil {
		itemsByType = make(map[string][]model.SearchTagItem)
	}
	hasOthers := facts.HasOthers
	if hasOthers == nil {
		hasOthers = make(map[string]bool)
	}

	for _, t := range tagTypes {
		if t == "Category" {
			sticky := getStickySearchTagValue(st, t)
			options := buildCategorySearchOptions(pid, sticky)
			if hasEffectiveSearchOptions(options) {
				tagMap[t] = options
				activeTitles[t] = allTitles[t]
				activeSortList = append(activeSortList, t)
			}
			continue
		}

		items := itemsByType[t]

		if t == "Sort" {
			tagMap[t] = HandleTagStr(t, false, defaultSortTagStrings...)
			activeTitles[t] = allTitles[t]
			activeSortList = append(activeSortList, t)
			continue
		}

		if len(items) == 0 && !shouldAlwaysExposeSearchTag(t) {
			continue
		}

		sticky := getStickySearchTagValue(st, t)
		options := FormatSearchTagItems(t, items, sticky, hasOthers[t])
		if hasEffectiveSearchOptions(options) || shouldAlwaysExposeSearchTag(t) {
			tagMap[t] = options
			activeTitles[t] = allTitles[t]
			activeSortList = append(activeSortList, t)
		}
	}

	res["titles"] = activeTitles
	res["sortList"] = activeSortList
	res["tags"] = tagMap

	return res
}

func GetSearchOptions(st model.SearchTagsVO) map[string]any {
	st = normalizeSearchTagsVO(st)
	full := GetSearchTag(st)
	tags, _ := full["tags"].(map[string]any)
	tagMap := make(map[string]any)
	for _, t := range []string{"Plot", "Area", "Language", "Year"} {
		tagMap[t] = tags[t]
	}
	return tagMap
}
