package film

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"
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
		items := loadSearchTagItemsByType(pid)[tagType]
		items = SortYearSearchTagItems(items)
		items = LimitSearchTagItems(items, SearchTagDisplayLimit)

		vals := make([]string, 0, len(items))
		for _, item := range items {
			vals = append(vals, item.Value)
		}
		return vals
	}

	var vals []string
	db.Mdb.Model(&model.SearchTagItem{}).
		Select("value").
		Where("pid = ? AND tag_type = ? AND score >= 5", pid, tagType).
		Order("score DESC, value ASC, id ASC").
		Limit(SearchTagDisplayLimit).
		Find(&vals)
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

func buildSearchTagCacheKey(st model.SearchTagsVO) string {
	st = normalizeSearchTagsVO(st)
	return fmt.Sprintf("%s:v%s:%d:%d:%s:%s:%s:%s:%s",
		config.SearchTags,
		getSearchTagsCacheVersion(),
		st.Pid, st.Cid,
		st.OriginalCategory, st.Area, st.Language, st.Year, st.Plot,
	)
}

func normalizeSearchTagsVO(st model.SearchTagsVO) model.SearchTagsVO {
	st.Pid = support.ResolveCategoryID(st.Pid)
	if st.Cid > 0 {
		st.Cid = support.ResolveCategoryID(st.Cid)
	}
	return st
}

func loadSearchTagItemsByType(pid int64) map[string][]model.SearchTagItem {
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
	items := loadSearchTagItemsByType(pid)[tagType]
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

func hasOthersSearchFacts(pid int64, tagType string) bool {
	pid = support.ResolveCategoryID(pid)
	if pid <= 0 {
		return false
	}

	query := db.Mdb.Model(&model.SearchInfo{}).Where("pid = ?", pid)
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
	cacheKey := buildSearchTagCacheKey(st)

	if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
		var res map[string]any
		if json.Unmarshal([]byte(data), &res) == nil {
			return res
		}
	}

	res := make(map[string]any)
	tagTypes := []string{"OriginalCategory", "Category", "Plot", "Area", "Language", "Year", "Sort"}
	allTitles := map[string]string{
		"OriginalCategory": "类别",
		"Category":         "类型",
		"Plot":             "剧情",
		"Area":             "地区",
		"Language":         "语言",
		"Year":             "年份",
		"Sort":             "排序",
	}

	tagMap := make(map[string]any)
	activeTitles := make(map[string]string)
	activeSortList := make([]string, 0)
	itemsByType := loadSearchTagItemsByType(pid)

	for _, t := range tagTypes {
		if t == "OriginalCategory" {
			sticky := getStickySearchTagValue(st, t)
			options := buildOriginalCategorySearchOptions(pid, sticky)
			if hasEffectiveSearchOptions(options) {
				tagMap[t] = options
				activeTitles[t] = allTitles[t]
				activeSortList = append(activeSortList, t)
			}
			continue
		}
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
		options := FormatSearchTagItems(t, items, sticky, hasOthersSearchFacts(pid, t))
		if hasEffectiveSearchOptions(options) || shouldAlwaysExposeSearchTag(t) {
			tagMap[t] = options
			activeTitles[t] = allTitles[t]
			activeSortList = append(activeSortList, t)
		}
	}

	res["titles"] = activeTitles
	res["sortList"] = activeSortList
	res["tags"] = tagMap

	if data, err := json.Marshal(res); err == nil {
		db.Rdb.Set(db.Cxt, cacheKey, string(data), time.Hour*2)
	}

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
