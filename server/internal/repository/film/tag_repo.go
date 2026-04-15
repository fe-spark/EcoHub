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

func GetTagsByTitle(pid int64, tagType string) []map[string]string {
	pid = support.ResolveCategoryID(pid)
	var tags []string
	var items []model.SearchTagItem

	db.Mdb.Where("pid = ? AND tag_type = ? AND score > 5", pid, tagType).
		Order("score DESC").Limit(30).Find(&items)

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
		Order("score DESC").
		Limit(SearchTagDisplayLimit).
		Find(&vals)
	return vals
}

func buildSearchTagCacheKey(st model.SearchTagsVO) string {
	st = normalizeSearchTagsVO(st)
	return fmt.Sprintf("%s:%d:%d:%s:%s:%s:%s",
		config.SearchTags,
		st.Pid, st.Cid,
		st.Area, st.Language, st.Year, st.Plot,
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
	db.Mdb.Where("pid = ? AND score > 0", pid).Order("score DESC").Find(&allItems)

	itemsByType := make(map[string][]model.SearchTagItem)
	for _, item := range allItems {
		itemsByType[item.TagType] = append(itemsByType[item.TagType], item)
	}
	return itemsByType
}

func getStickySearchTagValue(st model.SearchTagsVO, tagType string) string {
	switch tagType {
	case "Category":
		return fmt.Sprint(st.Cid)
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

func hasUncategorizedSearchInfo(pid int64) bool {
	pid = support.ResolveCategoryID(pid)
	if pid <= 0 {
		return false
	}
	var count int64
	db.Mdb.Model(&model.SearchInfo{}).Where("pid = ? AND cid = 0", pid).Count(&count)
	return count > 0
}

func hasUnknownYearSearchInfo(pid int64) bool {
	pid = support.ResolveCategoryID(pid)
	if pid <= 0 {
		return false
	}
	var count int64
	db.Mdb.Model(&model.SearchInfo{}).Where("pid = ? AND year = 0", pid).Count(&count)
	return count > 0
}

func hasUnknownTextSearchInfo(pid int64, column string) bool {
	pid = support.ResolveCategoryID(pid)
	if pid <= 0 || column == "" {
		return false
	}
	var count int64
	db.Mdb.Model(&model.SearchInfo{}).Where(fmt.Sprintf("pid = ? AND (%s = '' OR %s IS NULL)", column, column), pid).Count(&count)
	return count > 0
}

func appendSpecialSearchOptions(tagType string, formatted []map[string]string, st model.SearchTagsVO) []map[string]string {
	switch tagType {
	case "Category":
		if hasUncategorizedSearchInfo(st.Pid) || st.Cid == model.TagUncategorizedValue {
			return AppendSearchOption(formatted, map[string]string{
				"Name":  model.TagUncategorizedName,
				"Value": fmt.Sprint(model.TagUncategorizedValue),
			})
		}
	case "Year":
		if hasUnknownYearSearchInfo(st.Pid) || st.Year == model.TagUnknownValue {
			return AppendSearchOption(formatted, map[string]string{
				"Name":  model.TagUnknownName,
				"Value": model.TagUnknownValue,
			})
		}
	case "Area":
		if hasUnknownTextSearchInfo(st.Pid, "area") || st.Area == model.TagUnknownValue {
			return AppendSearchOption(formatted, map[string]string{
				"Name":  model.TagUnknownName,
				"Value": model.TagUnknownValue,
			})
		}
	case "Language":
		if hasUnknownTextSearchInfo(st.Pid, "language") || st.Language == model.TagUnknownValue {
			return AppendSearchOption(formatted, map[string]string{
				"Name":  model.TagUnknownName,
				"Value": model.TagUnknownValue,
			})
		}
	case "Plot":
		if hasUnknownTextSearchInfo(st.Pid, "class_tag") || st.Plot == model.TagUnknownValue {
			return AppendSearchOption(formatted, map[string]string{
				"Name":  model.TagUnknownName,
				"Value": model.TagUnknownValue,
			})
		}
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
	tagTypes := []string{"Category", "Plot", "Area", "Language", "Year", "Sort"}
	res["titles"] = map[string]string{
		"Category": "类型",
		"Plot":     "剧情",
		"Area":     "地区",
		"Language": "语言",
		"Year":     "年份",
		"Sort":     "排序",
	}

	tagMap := make(map[string]any)
	activeSortList := make([]string, 0)
	itemsByType := loadSearchTagItemsByType(pid)

	for _, t := range tagTypes {
		items := itemsByType[t]

		if t == "Sort" {
			tagMap[t] = HandleTagStr(t, false, defaultSortTagStrings...)
			activeSortList = append(activeSortList, t)
			continue
		}

		if len(items) == 0 {
			if t == "Category" || t == "Year" || t == "Area" || t == "Language" || t == "Plot" {
				tagMap[t] = appendSpecialSearchOptions(t, HandleTagStr(t, true), st)
				activeSortList = append(activeSortList, t)
			}
			continue
		}

		sticky := getStickySearchTagValue(st, t)
		tagMap[t] = appendSpecialSearchOptions(t, FormatSearchTagItems(t, items, sticky), st)
		activeSortList = append(activeSortList, t)
	}

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
