package snapshot

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/film/shared"
	"server/internal/repository/support"
)

// searchTagBackfill 从已入库影片补齐剧情、地区、语言、年份。由 writer 注册，避免 snapshot 反向依赖 writer。
var searchTagBackfill func(pid int64) error

func SetSearchTagBackfill(fn func(pid int64) error) {
	searchTagBackfill = fn
}

var filterOptionTagTypes = []string{"Plot", "Area", "Language", "Year"}

func emptyFilterOptionResponse() map[string]any {
	return map[string]any{
		"titles":   map[string]string{"Sort": "排序"},
		"sortList": []string{"Sort"},
		"tags": map[string]any{
			"Sort": shared.HandleTagStr("Sort", false, shared.DefaultSortTagStrings...),
		},
	}
}

func GetFilterOptionSnapshot(version string, pid int64, sourceIdOpt ...string) map[string]any {
	var sourceId string
	if len(sourceIdOpt) > 0 {
		sourceId = strings.TrimSpace(sourceIdOpt[0])
	}
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if sourceId != "" {
		return sourceTypeFilterOptions(version, sourceId, pid)
	}
	pid = support.ResolveCategoryID(pid)
	if pid <= 0 {
		return emptyFilterOptionResponse()
	}

	var cacheKey string
	if sourceId != "" {
		cacheKey = fmt.Sprintf("%s:src_%s:v%s:%d", config.FilmFilterOptionKey, sourceId, version, pid)
	} else {
		cacheKey = fmt.Sprintf("%s:v%s:%d", config.FilmFilterOptionKey, version, pid)
	}
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached map[string]any
			if json.Unmarshal([]byte(data), &cached) == nil && cachedFilterHasDynamicTags(cached) {
				return cached
			}
		}
	}

	if db.Mdb == nil {
		return emptyFilterOptionResponse()
	}
	listGen := GetSearchCacheVersion()

	// 1. 获取子分类
	var subCats []model.Category
	if sourceId != "" {
		var mappedCategoryIDs []int64
		_ = db.Mdb.Model(&model.CategoryMapping{}).
			Where("source_id = ? AND category_id > 0", sourceId).
			Pluck("category_id", &mappedCategoryIDs).Error
		if len(mappedCategoryIDs) > 0 {
			_ = db.Mdb.Where("pid = ? AND id IN ? AND `show` = ?", pid, mappedCategoryIDs, true).
				Order("sort ASC, id ASC").Find(&subCats).Error
		}
	}
	if len(subCats) == 0 {
		_ = db.Mdb.Where("pid = ? AND `show` = ?", pid, true).Order("sort ASC, id ASC").Find(&subCats).Error
	}
	catItems := []map[string]string{{"Name": "全部", "Value": ""}}
	for _, c := range subCats {
		catItems = append(catItems, map[string]string{
			"Name":  c.Name,
			"Value": fmt.Sprint(c.Id),
		})
	}

	// 2. 获取标签 (Plot, Area, Language, Year)。没有这些行时，从已经入库的影片补齐。
	var tagRows []model.SearchTagItem
	_ = db.Mdb.Where("pid = ?", pid).Order("score DESC, id ASC").Find(&tagRows).Error
	if !rawFilterHasDynamicTags(tagRows) && searchTagBackfill != nil {
		if err := searchTagBackfill(pid); err != nil {
			log.Printf("[SearchTags] 按入库影片补齐筛选标签失败 pid=%d err=%v", pid, err)
		} else {
			tagRows = nil
			_ = db.Mdb.Where("pid = ?", pid).Order("score DESC, id ASC").Find(&tagRows).Error
		}
	}
	itemsByType := make(map[string][]model.SearchTagItem)
	for _, item := range tagRows {
		itemsByType[item.TagType] = append(itemsByType[item.TagType], item)
	}

	tags := make(map[string]any)
	titles := make(map[string]string)
	sortList := make([]string, 0)
	titleNames := map[string]string{
		"Category": "类型",
		"Plot":     "剧情",
		"Area":     "地区",
		"Language": "语言",
		"Year":     "年份",
		"Sort":     "排序",
	}

	if len(catItems) > 1 {
		tags["Category"] = catItems
		titles["Category"] = titleNames["Category"]
		sortList = append(sortList, "Category")
	}

	for _, tagType := range filterOptionTagTypes {
		items := shared.FormatSearchTagItems(tagType, itemsByType[tagType], "", false)
		if len(items) > 1 {
			tags[tagType] = items
			titles[tagType] = titleNames[tagType]
			sortList = append(sortList, tagType)
		}
	}

	// 3. 排序选项
	sortItems := shared.HandleTagStr("Sort", false, shared.DefaultSortTagStrings...)
	tags["Sort"] = sortItems
	titles["Sort"] = titleNames["Sort"]
	sortList = append(sortList, "Sort")

	res := map[string]any{
		"titles":   titles,
		"sortList": sortList,
		"tags":     tags,
	}

	if raw, err := json.Marshal(res); err == nil {
		writeListCache(cacheKey, raw, 10*time.Minute, listGen)
	}
	return res
}

func sourceTypeFilterKeys(sourceID string, typeID int64) []string {
	ids := repository.PublicSourceTypeIDs(sourceID, "pid", typeID)
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		key := support.BuildSourceCategoryKey(sourceID, id)
		if key != "" {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return []string{""}
	}
	return keys
}

// sourceTypeFilterOptions 筛选项跟当前站的 type_id 走。子类是该站自己的下级 type_id，剧情地区从这些片子上统计。
func sourceTypeFilterOptions(version, sourceId string, typeID int64) map[string]any {
	key := support.BuildSourceCategoryKey(sourceId, typeID)
	if key == "" {
		return emptyFilterOptionResponse()
	}
	cacheKey := fmt.Sprintf("%s:stvis:src_%s:v%s:r%s:%d", config.FilmFilterOptionKey, sourceId, version, support.GetRuleVersion(), typeID)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached map[string]any
			if json.Unmarshal([]byte(data), &cached) == nil && cachedFilterHasDynamicTags(cached) {
				return cached
			}
		}
	}
	if db.Mdb == nil {
		return emptyFilterOptionResponse()
	}
	listGen := GetSearchCacheVersion()

	catItems := []map[string]string{{"Name": "全部", "Value": ""}}
	for _, child := range repository.PublicSourceChildNodes(sourceId, typeID) {
		if child == nil || child.Id <= 0 || strings.TrimSpace(child.Name) == "" {
			continue
		}
		catItems = append(catItems, map[string]string{
			"Name":  child.Name,
			"Value": fmt.Sprint(child.Id),
		})
	}

	type filmFilterRow struct {
		ClassTag string
		Area     string
		Language string
		Year     int64
	}
	filterKeys := sourceTypeFilterKeys(sourceId, typeID)
	var films []filmFilterRow
	_ = db.Mdb.Model(&model.FilmIndex{}).
		Select("class_tag, area, language, year").
		Where("root_category_key IN ? OR category_key IN ?", filterKeys, filterKeys).
		Find(&films).Error

	counts := map[string]map[string]int{
		"Plot":     {},
		"Area":     {},
		"Language": {},
		"Year":     {},
	}
	for _, film := range films {
		for _, part := range shared.SplitRawSearchTags(film.ClassTag) {
			addSourceTypeTagCount(counts["Plot"], shared.NormalizeSearchTagValue("Plot", part))
		}
		addSourceTypeTagCount(counts["Area"], shared.NormalizeSearchTagValue("Area", film.Area))
		addSourceTypeTagCount(counts["Language"], shared.NormalizeSearchTagValue("Language", film.Language))
		if film.Year > 0 {
			addSourceTypeTagCount(counts["Year"], fmt.Sprint(film.Year))
		}
	}

	tags := make(map[string]any)
	titles := make(map[string]string)
	sortList := make([]string, 0)
	titleNames := map[string]string{
		"Category": "类型",
		"Plot":     "剧情",
		"Area":     "地区",
		"Language": "语言",
		"Year":     "年份",
		"Sort":     "排序",
	}
	if len(catItems) > 1 {
		tags["Category"] = catItems
		titles["Category"] = titleNames["Category"]
		sortList = append(sortList, "Category")
	}
	for _, tagType := range filterOptionTagTypes {
		items := shared.FormatSearchTagItems(tagType, sourceTypeTagItems(tagType, counts[tagType]), "", false)
		if len(items) > 1 {
			tags[tagType] = items
			titles[tagType] = titleNames[tagType]
			sortList = append(sortList, tagType)
		}
	}
	sortItems := shared.HandleTagStr("Sort", false, shared.DefaultSortTagStrings...)
	tags["Sort"] = sortItems
	titles["Sort"] = titleNames["Sort"]
	sortList = append(sortList, "Sort")

	res := map[string]any{
		"titles":   titles,
		"sortList": sortList,
		"tags":     tags,
	}
	if raw, err := json.Marshal(res); err == nil {
		writeListCache(cacheKey, raw, 10*time.Minute, listGen)
	}
	return res
}

func addSourceTypeTagCount(counts map[string]int, value string) {
	value = strings.TrimSpace(value)
	if value == "" || value == model.TagOthersValue || value == "其他" || value == "其它" || value == "全部" {
		return
	}
	counts[value]++
}

func sourceTypeTagItems(tagType string, counts map[string]int) []model.SearchTagItem {
	items := make([]model.SearchTagItem, 0, len(counts))
	for value, score := range counts {
		items = append(items, model.SearchTagItem{
			TagType: tagType,
			Name:    value,
			Value:   value,
			Score:   int64(score),
		})
	}
	return items
}

func rawFilterHasDynamicTags(rows []model.SearchTagItem) bool {
	for _, row := range rows {
		if isDynamicFilterType(row.TagType) {
			return true
		}
	}
	return false
}

func cachedFilterHasDynamicTags(cached map[string]any) bool {
	raw, ok := cached["sortList"]
	if !ok {
		return false
	}
	switch list := raw.(type) {
	case []string:
		for _, item := range list {
			if isDynamicFilterType(item) {
				return true
			}
		}
	case []any:
		for _, item := range list {
			name, _ := item.(string)
			if isDynamicFilterType(name) {
				return true
			}
		}
	}
	return false
}

func isDynamicFilterType(tagType string) bool {
	switch tagType {
	case "Plot", "Area", "Language", "Year":
		return true
	default:
		return false
	}
}

func GetAdminFilterOptionSnapshots() map[int64]map[string]any {
	version := GetActiveSnapshotVersion()
	cacheKey := fmt.Sprintf("%s:Admin:v%s", config.FilmFilterOptionKey, version)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached map[int64]map[string]any
			if json.Unmarshal([]byte(data), &cached) == nil && len(cached) > 0 {
				return cached
			}
		}
	}

	if db.Mdb == nil {
		return map[int64]map[string]any{}
	}
	listGen := GetSearchCacheVersion()

	var roots []model.Category
	_ = db.Mdb.Where("pid = ? AND `show` = ?", 0, true).Order("sort ASC, id ASC").Find(&roots).Error
	result := make(map[int64]map[string]any, len(roots))
	for _, root := range roots {
		pid := support.ResolveCategoryID(root.Id)
		if pid <= 0 {
			continue
		}
		resp := GetFilterOptionSnapshot(version, pid)
		if tags, ok := resp["tags"].(map[string]any); ok && tags != nil {
			result[pid] = tags
		}
	}

	if raw, err := json.Marshal(result); err == nil {
		writeListCache(cacheKey, raw, listCacheTTL(len(result), 10*time.Minute), listGen)
	}
	return result
}
