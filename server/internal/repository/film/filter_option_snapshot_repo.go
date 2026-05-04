package film

import (
	"fmt"
	"log"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

var filterOptionTagTypes = []string{"Plot", "Area", "Language", "Year"}

var filterOptionResponseOrder = []string{"Category", "Plot", "Area", "Language", "Year", "Sort"}

func emptyFilterOptionResponse() map[string]any {
	return map[string]any{
		"titles":   map[string]string{},
		"sortList": []string{},
		"tags":     map[string]any{},
	}
}

func RebuildFilterOptionSnapshot(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil
	}

	startedAt := time.Now()
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("snapshot_version = ?", version).Unscoped().Delete(&model.FilmFilterOptionSnapshot{}).Error; err != nil {
			return err
		}

		var roots []model.Category
		if err := tx.Where("pid = ? AND `show` = ?", 0, true).Order("sort ASC, id ASC").Find(&roots).Error; err != nil {
			return err
		}

		options := make([]model.FilmFilterOptionSnapshot, 0)
		for _, root := range roots {
			pid := support.ResolveCategoryID(root.Id)
			if pid <= 0 {
				continue
			}
			options = append(options, buildCategoryFilterOptionsFromReadModel(version, pid)...)
			itemsByType := loadSearchTagItemsByTypeFromReadModel(version, pid)
			for _, tagType := range filterOptionTagTypes {
				options = append(options, buildTagFilterOptions(version, pid, tagType, itemsByType[tagType])...)
			}
			options = append(options, buildSortFilterOptions(version, pid)...)
		}

		if len(options) == 0 {
			return nil
		}
		return tx.CreateInBatches(options, 1000).Error
	})
	if err != nil {
		return err
	}
	log.Printf("[FilterOptionSnapshot] 重建完成 version=%s cost=%s", version, time.Since(startedAt))
	return nil
}

func buildCategoryFilterOptionsFromReadModel(version string, pid int64) []model.FilmFilterOptionSnapshot {
	options := []model.FilmFilterOptionSnapshot{{
		SnapshotVersion: version,
		Pid:             pid,
		TagType:         "Category",
		Name:            "全部",
		Value:           "",
		Score:           0,
		Sort:            0,
	}}

	readModel := GetActiveFilmReadModel()
	if readModel == nil || readModel.Version != version {
		return options
	}
	counts := make(map[int64]int64)
	for _, snapshot := range readModel.projectedSnapshotsByPid(pid) {
		if snapshot.Cid <= 0 {
			continue
		}
		counts[support.ResolveCategoryID(snapshot.Cid)]++
	}

	var categories []model.Category
	if err := db.Mdb.Where("pid = ? AND `show` = ?", pid, true).Order("sort ASC, id ASC").Find(&categories).Error; err != nil {
		return options
	}
	for index, category := range categories {
		resolvedID := support.ResolveCategoryID(category.Id)
		if counts[resolvedID] <= 0 {
			continue
		}
		options = append(options, model.FilmFilterOptionSnapshot{
			SnapshotVersion: version,
			Pid:             pid,
			TagType:         "Category",
			Name:            category.Name,
			Value:           fmt.Sprint(category.Id),
			Score:           counts[resolvedID],
			Sort:            index + 1,
		})
	}
	return options
}

func loadSearchTagItemsByTypeFromReadModel(version string, pid int64) map[string][]model.SearchTagItem {
	itemsByType := make(map[string][]model.SearchTagItem)
	readModel := GetActiveFilmReadModel()
	if readModel == nil || readModel.Version != version || pid <= 0 {
		return itemsByType
	}
	areaCounts := make(map[string]int64)
	languageCounts := make(map[string]int64)
	yearCounts := make(map[string]int64)
	plotCounts := make(map[string]int64)
	for _, snapshot := range readModel.projectedSnapshotsByPid(pid) {
		if value := normalizeSearchTagValue("Area", snapshot.Area); value != "" {
			areaCounts[value]++
		}
		if value := normalizeSearchTagValue("Language", snapshot.Language); value != "" {
			languageCounts[value]++
		}
		if snapshot.Year > 0 {
			yearCounts[fmt.Sprint(snapshot.Year)]++
		}
		for _, tag := range splitClassTags(snapshot.ClassTag) {
			if value := normalizeSearchTagValue("Plot", tag); value != "" {
				plotCounts[value]++
			}
		}
	}
	itemsByType["Area"] = searchTagItemsFromCounts("Area", areaCounts)
	itemsByType["Language"] = searchTagItemsFromCounts("Language", languageCounts)
	itemsByType["Year"] = searchTagItemsFromCounts("Year", yearCounts)
	itemsByType["Plot"] = searchTagItemsFromCounts("Plot", plotCounts)
	return itemsByType
}

func searchTagItemsFromCounts(tagType string, counts map[string]int64) []model.SearchTagItem {
	items := make([]model.SearchTagItem, 0, len(counts))
	for value, score := range counts {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		items = append(items, model.SearchTagItem{TagType: tagType, Name: value, Value: value, Score: score})
	}
	items = SortSearchTagItems(tagType, items)
	return items
}

func buildTagFilterOptions(version string, pid int64, tagType string, items []model.SearchTagItem) []model.FilmFilterOptionSnapshot {
	formatted := formatFilterOptionItems(tagType, items)
	options := make([]model.FilmFilterOptionSnapshot, 0, len(formatted))
	for index, item := range formatted {
		name := strings.TrimSpace(item["Name"])
		value := strings.TrimSpace(item["Value"])
		if name == "" && value == "" {
			continue
		}
		options = append(options, model.FilmFilterOptionSnapshot{
			SnapshotVersion: version,
			Pid:             pid,
			TagType:         tagType,
			Name:            name,
			Value:           value,
			Score:           int64(len(formatted) - index),
			Sort:            index,
		})
	}
	return options
}

func formatFilterOptionItems(tagType string, items []model.SearchTagItem) []map[string]string {
	normalItems, _ := SplitSearchTagItems(tagType, items)
	normalItems = SortSearchTagItems(tagType, normalItems)
	normalItems = LimitSearchTagItems(normalItems, SearchTagDisplayLimit)

	tagStrs := make([]string, 0, len(normalItems))
	for _, item := range normalItems {
		name := strings.TrimSpace(item.Name)
		value := strings.TrimSpace(item.Value)
		if name == "" || value == "" {
			continue
		}
		tagStrs = append(tagStrs, fmt.Sprintf("%s:%s", name, value))
	}
	return HandleTagStr(tagType, true, tagStrs...)
}

func buildSortFilterOptions(version string, pid int64) []model.FilmFilterOptionSnapshot {
	formatted := HandleTagStr("Sort", false, defaultSortTagStrings...)
	options := make([]model.FilmFilterOptionSnapshot, 0, len(formatted))
	for index, item := range formatted {
		options = append(options, model.FilmFilterOptionSnapshot{
			SnapshotVersion: version,
			Pid:             pid,
			TagType:         "Sort",
			Name:            item["Name"],
			Value:           item["Value"],
			Score:           int64(len(formatted) - index),
			Sort:            index,
		})
	}
	return options
}

func GetFilterOptionSnapshot(version string, pid int64) map[string]any {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveReadModelVersion()
	}
	pid = support.ResolveCategoryID(pid)
	if version == "" || pid <= 0 {
		return emptyFilterOptionResponse()
	}
	readModel := GetActiveFilmReadModel()
	if readModel == nil || readModel.Version != version {
		return emptyFilterOptionResponse()
	}
	projected := ensureProjectedFilmReadModel(readModel)
	if response := projected.FilterOptions[pid]; response != nil {
		return response
	}
	return emptyFilterOptionResponse()
}

func EnsureActiveFilterOptionSnapshot() error {
	version := GetActiveReadModelVersion()
	if version == "" {
		return nil
	}

	var count int64
	if err := db.Mdb.Model(&model.FilmFilterOptionSnapshot{}).Where("snapshot_version = ?", version).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return RebuildFilterOptionSnapshot(version)
}

func buildFilterOptionResponse(rows []model.FilmFilterOptionSnapshot) map[string]any {
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

	grouped := make(map[string][]map[string]string)
	for _, row := range rows {
		list := grouped[row.TagType]
		list = append(list, map[string]string{"Name": row.Name, "Value": row.Value})
		grouped[row.TagType] = list
	}

	for _, tagType := range filterOptionResponseOrder {
		list := grouped[tagType]
		if !hasRealFilterOptionItems(tagType, list) {
			continue
		}
		tags[tagType] = list
		titles[tagType] = titleNames[tagType]
		sortList = append(sortList, tagType)
	}

	return map[string]any{
		"titles":   titles,
		"sortList": sortList,
		"tags":     tags,
	}
}

func hasRealFilterOptionItems(tagType string, list []map[string]string) bool {
	if len(list) == 0 {
		return false
	}
	if tagType == "Sort" {
		for _, item := range list {
			if strings.TrimSpace(item["Value"]) != "" {
				return true
			}
		}
		return false
	}

	for _, item := range list {
		if strings.TrimSpace(item["Value"]) != "" {
			return true
		}
	}
	return false
}

func GetAdminFilterOptionSnapshots() map[int64]map[string]any {
	version := GetActiveReadModelVersion()
	if version == "" {
		return map[int64]map[string]any{}
	}
	readModel := GetActiveFilmReadModel()
	if readModel == nil || readModel.Version != version {
		return map[int64]map[string]any{}
	}
	projected := ensureProjectedFilmReadModel(readModel)
	result := make(map[int64]map[string]any, len(projected.FilterOptions))
	for pid, response := range projected.FilterOptions {
		tags, _ := response["tags"].(map[string]any)
		result[pid] = tags
	}
	return result
}

func buildRuntimeFilterOptionRows(version string, pid int64) []model.FilmFilterOptionSnapshot {
	rows := make([]model.FilmFilterOptionSnapshot, 0)
	rows = append(rows, buildCategoryFilterOptionsFromReadModel(version, pid)...)
	itemsByType := loadSearchTagItemsByTypeFromReadModel(version, pid)
	for _, tagType := range filterOptionTagTypes {
		rows = append(rows, buildTagFilterOptions(version, pid, tagType, itemsByType[tagType])...)
	}
	rows = append(rows, buildSortFilterOptions(version, pid)...)
	return rows
}

func buildProjectedFilterOptionResponses(version string, projected *ProjectedFilmReadModel) map[int64]map[string]any {
	var roots []model.Category
	if err := db.Mdb.Where("pid = ? AND `show` = ?", 0, true).Order("sort ASC, id ASC").Find(&roots).Error; err != nil {
		log.Printf("BuildProjectedFilterOptionResponses Error: %v", err)
		return map[int64]map[string]any{}
	}

	responses := make(map[int64]map[string]any, len(roots))
	for _, root := range roots {
		pid := support.ResolveCategoryID(root.Id)
		if pid <= 0 {
			continue
		}
		responses[pid] = buildFilterOptionResponse(buildProjectedFilterOptionRows(version, pid, projected))
	}
	return responses
}

func buildProjectedFilterOptionRows(version string, pid int64, projected *ProjectedFilmReadModel) []model.FilmFilterOptionSnapshot {
	rows := make([]model.FilmFilterOptionSnapshot, 0)
	rows = append(rows, buildCategoryFilterOptionsFromProjectedReadModel(version, pid, projected)...)
	itemsByType := loadSearchTagItemsByTypeFromProjectedReadModel(pid, projected)
	for _, tagType := range filterOptionTagTypes {
		rows = append(rows, buildTagFilterOptions(version, pid, tagType, itemsByType[tagType])...)
	}
	rows = append(rows, buildSortFilterOptions(version, pid)...)
	return rows
}

func buildCategoryFilterOptionsFromProjectedReadModel(version string, pid int64, projected *ProjectedFilmReadModel) []model.FilmFilterOptionSnapshot {
	options := []model.FilmFilterOptionSnapshot{{
		SnapshotVersion: version,
		Pid:             pid,
		TagType:         "Category",
		Name:            "全部",
		Value:           "",
		Score:           0,
		Sort:            0,
	}}

	counts := make(map[int64]int64)
	for _, mid := range projected.ByPid[pid] {
		snapshot, ok := projected.ByMid[mid]
		if !ok || snapshot.Cid <= 0 {
			continue
		}
		counts[support.ResolveCategoryID(snapshot.Cid)]++
	}

	var categories []model.Category
	if err := db.Mdb.Where("pid = ? AND `show` = ?", pid, true).Order("sort ASC, id ASC").Find(&categories).Error; err != nil {
		return options
	}
	for index, category := range categories {
		resolvedID := support.ResolveCategoryID(category.Id)
		if counts[resolvedID] <= 0 {
			continue
		}
		options = append(options, model.FilmFilterOptionSnapshot{
			SnapshotVersion: version,
			Pid:             pid,
			TagType:         "Category",
			Name:            category.Name,
			Value:           fmt.Sprint(category.Id),
			Score:           counts[resolvedID],
			Sort:            index + 1,
		})
	}
	return options
}

func loadSearchTagItemsByTypeFromProjectedReadModel(pid int64, projected *ProjectedFilmReadModel) map[string][]model.SearchTagItem {
	itemsByType := make(map[string][]model.SearchTagItem)
	if projected == nil || pid <= 0 {
		return itemsByType
	}
	areaCounts := make(map[string]int64)
	languageCounts := make(map[string]int64)
	yearCounts := make(map[string]int64)
	plotCounts := make(map[string]int64)
	for _, mid := range projected.ByPid[pid] {
		snapshot, ok := projected.ByMid[mid]
		if !ok {
			continue
		}
		if value := normalizeSearchTagValue("Area", snapshot.Area); value != "" {
			areaCounts[value]++
		}
		if value := normalizeSearchTagValue("Language", snapshot.Language); value != "" {
			languageCounts[value]++
		}
		if snapshot.Year > 0 {
			yearCounts[fmt.Sprint(snapshot.Year)]++
		}
		for _, tag := range splitClassTags(snapshot.ClassTag) {
			if value := normalizeSearchTagValue("Plot", tag); value != "" {
				plotCounts[value]++
			}
		}
	}
	itemsByType["Area"] = searchTagItemsFromCounts("Area", areaCounts)
	itemsByType["Language"] = searchTagItemsFromCounts("Language", languageCounts)
	itemsByType["Year"] = searchTagItemsFromCounts("Year", yearCounts)
	itemsByType["Plot"] = searchTagItemsFromCounts("Plot", plotCounts)
	return itemsByType
}
