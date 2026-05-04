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
			options = append(options, buildCategoryFilterOptions(tx, version, pid)...)
			itemsByType := loadLegacySearchTagItemsByType(pid)
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

func buildCategoryFilterOptions(tx *gorm.DB, version string, pid int64) []model.FilmFilterOptionSnapshot {
	options := []model.FilmFilterOptionSnapshot{{
		SnapshotVersion: version,
		Pid:             pid,
		TagType:         "Category",
		Name:            "全部",
		Value:           "",
		Score:           0,
		Sort:            0,
	}}

	var categories []model.Category
	tx.Where("pid = ? AND `show` = ?", pid, true).Order("sort ASC, id ASC").Find(&categories)
	for index, category := range categories {
		options = append(options, model.FilmFilterOptionSnapshot{
			SnapshotVersion: version,
			Pid:             pid,
			TagType:         "Category",
			Name:            category.Name,
			Value:           fmt.Sprint(category.Id),
			Score:           int64(len(categories) - index),
			Sort:            index + 1,
		})
	}
	return options
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
	pid = support.ResolveCategoryID(pid)
	if version == "" || pid <= 0 {
		return map[string]any{}
	}

	var rows []model.FilmFilterOptionSnapshot
	if err := db.Mdb.Where("snapshot_version = ? AND pid = ?", version, pid).
		Order("sort ASC, score DESC, id ASC").Find(&rows).Error; err != nil {
		log.Printf("GetFilterOptionSnapshot Error: %v", err)
		return map[string]any{}
	}

	return buildFilterOptionResponse(rows)
}

func EnsureActiveFilterOptionSnapshot() error {
	version := GetActiveSnapshotVersion()
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
		if len(list) == 0 {
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

func GetAdminFilterOptionSnapshots() map[int64]map[string]any {
	version := GetActiveSnapshotVersion()
	if version == "" {
		return map[int64]map[string]any{}
	}

	var rows []model.FilmFilterOptionSnapshot
	if err := db.Mdb.Where("snapshot_version = ?", version).
		Order("pid ASC, sort ASC, score DESC, id ASC").Find(&rows).Error; err != nil {
		log.Printf("GetAdminFilterOptionSnapshots Error: %v", err)
		return map[int64]map[string]any{}
	}

	grouped := make(map[int64][]model.FilmFilterOptionSnapshot)
	for _, row := range rows {
		grouped[row.Pid] = append(grouped[row.Pid], row)
	}

	result := make(map[int64]map[string]any, len(grouped))
	for pid, groupRows := range grouped {
		response := buildFilterOptionResponse(groupRows)
		tags, _ := response["tags"].(map[string]any)
		result[pid] = tags
	}
	return result
}
