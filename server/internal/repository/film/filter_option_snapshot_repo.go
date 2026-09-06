package film

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"
)

var filterOptionTagTypes = []string{"Plot", "Area", "Language", "Year"}
var filterOptionResponseOrder = []string{"Category", "Plot", "Area", "Language", "Year", "Sort"}

func emptyFilterOptionResponse() map[string]any {
	return map[string]any{
		"titles":   map[string]string{"Sort": "排序"},
		"sortList": []string{"Sort"},
		"tags": map[string]any{
			"Sort": HandleTagStr("Sort", false, defaultSortTagStrings...),
		},
	}
}

func GetFilterOptionSnapshot(version string, pid int64) map[string]any {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	pid = support.ResolveCategoryID(pid)
	if pid <= 0 {
		return emptyFilterOptionResponse()
	}

	cacheKey := fmt.Sprintf("EcoHub:filter_option:v%s:%d", version, pid)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached map[string]any
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached
			}
		}
	}

	if db.Mdb == nil {
		return emptyFilterOptionResponse()
	}

	// 1. 获取子分类
	var subCats []model.Category
	_ = db.Mdb.Where("pid = ? AND `show` = ?", pid, true).Order("sort ASC, id ASC").Find(&subCats).Error
	catItems := []map[string]string{{"Name": "全部", "Value": ""}}
	for _, c := range subCats {
		catItems = append(catItems, map[string]string{
			"Name":  c.Name,
			"Value": fmt.Sprint(c.Id),
		})
	}

	// 2. 获取标签 (Plot, Area, Language, Year)
	var tagRows []model.SearchTagItem
	_ = db.Mdb.Where("pid = ?", pid).Order("score DESC, id ASC").Find(&tagRows).Error
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
		items := formatSearchTagItems(tagType, itemsByType[tagType], "", false, SearchTagDisplayLimit)
		if len(items) > 1 {
			tags[tagType] = items
			titles[tagType] = titleNames[tagType]
			sortList = append(sortList, tagType)
		}
	}

	// 3. 排序选项
	sortItems := HandleTagStr("Sort", false, defaultSortTagStrings...)
	tags["Sort"] = sortItems
	titles["Sort"] = titleNames["Sort"]
	sortList = append(sortList, "Sort")

	res := map[string]any{
		"titles":   titles,
		"sortList": sortList,
		"tags":     tags,
	}

	if db.Rdb != nil {
		if raw, err := json.Marshal(res); err == nil {
			_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), 10*time.Minute).Err()
		}
	}
	return res
}

func GetAdminFilterOptionSnapshots() map[int64]map[string]any {
	version := GetActiveSnapshotVersion()
	cacheKey := fmt.Sprintf("EcoHub:filter_option:admin:v%s", version)
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

	if db.Rdb != nil && len(result) > 0 {
		if raw, err := json.Marshal(result); err == nil {
			_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), 10*time.Minute).Err()
		}
	}
	return result
}
