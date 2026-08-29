package film

import (
	"encoding/json"
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
	return buildFilterOptionResponse(buildSortFilterOptions("", 0))
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
			options = append(options, buildCategoryFilterOptionsFromDB(tx, version, pid)...)
			itemsByType := loadSearchTagItemsByTypeFromDB(tx, version, pid)
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

func buildCategoryFilterOptionsFromDB(tx *gorm.DB, version string, pid int64) []model.FilmFilterOptionSnapshot {
	options := []model.FilmFilterOptionSnapshot{{
		SnapshotVersion: version,
		Pid:             pid,
		TagType:         "Category",
		Name:            "全部",
		Value:           "",
		Score:           0,
		Sort:            0,
	}}

	type cidCount struct {
		Cid   int64
		Count int64
	}
	var counts []cidCount
	tx.Model(&model.FilmListSnapshot{}).
		Select("cid, count(1) as count").
		Where("snapshot_version = ? AND pid = ? AND cid > 0", version, pid).
		Group("cid").
		Scan(&counts)

	countMap := make(map[int64]int64, len(counts))
	for _, c := range counts {
		countMap[support.ResolveCategoryID(c.Cid)] += c.Count
	}

	var categories []model.Category
	if err := tx.Where("pid = ? AND `show` = ?", pid, true).Order("sort ASC, id ASC").Find(&categories).Error; err != nil {
		return options
	}
	for index, category := range categories {
		resolvedID := support.ResolveCategoryID(category.Id)
		if countMap[resolvedID] <= 0 {
			continue
		}
		options = append(options, model.FilmFilterOptionSnapshot{
			SnapshotVersion: version,
			Pid:             pid,
			TagType:         "Category",
			Name:            category.Name,
			Value:           fmt.Sprint(category.Id),
			Score:           countMap[resolvedID],
			Sort:            index + 1,
		})
	}
	return options
}

func loadSearchTagItemsByTypeFromDB(tx *gorm.DB, version string, pid int64) map[string][]model.SearchTagItem {
	areaCounts := make(map[string]int64)
	languageCounts := make(map[string]int64)
	yearCounts := make(map[string]int64)
	plotCounts := make(map[string]int64)

	var batchRows []model.FilmListSnapshot
	err := tx.Model(&model.FilmListSnapshot{}).
		Select("id, area, language, year, class_tag").
		Where("snapshot_version = ? AND pid = ?", version, pid).
		FindInBatches(&batchRows, 2000, func(batchTx *gorm.DB, batch int) error {
			for _, r := range batchRows {
				countSingleSearchTag("Area", r.Area, areaCounts)
				countSingleSearchTag("Language", r.Language, languageCounts)
				if r.Year > 0 {
					yearCounts[fmt.Sprint(r.Year)]++
				} else {
					yearCounts[model.TagUnknownValue]++
				}
				countPlotSearchTags(r.ClassTag, plotCounts)
			}
			return nil
		}).Error

	if err != nil {
		log.Printf("[FilterOptionSnapshot] loadSearchTagItemsByTypeFromDB failed: %v", err)
	}

	return map[string][]model.SearchTagItem{
		"Area":     searchTagItemsFromCounts("Area", areaCounts),
		"Language": searchTagItemsFromCounts("Language", languageCounts),
		"Year":     searchTagItemsFromCounts("Year", yearCounts),
		"Plot":     searchTagItemsFromCounts("Plot", plotCounts),
	}
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
	return formatSearchTagItems(tagType, items, "", false, SearchTagDisplayLimit)
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
		version = GetActiveSnapshotVersion()
	}
	pid = support.ResolveCategoryID(pid)
	if version == "" || pid <= 0 {
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

	var rows []model.FilmFilterOptionSnapshot
	if err := db.Mdb.Where("snapshot_version = ? AND pid = ?", version, pid).Order("sort ASC, id ASC").Find(&rows).Error; err != nil {
		log.Printf("[FilterOptionSnapshot] query failed version=%s pid=%d: %v", version, pid, err)
		return emptyFilterOptionResponse()
	}
	if len(rows) == 0 {
		return emptyFilterOptionResponse()
	}

	res := buildFilterOptionResponse(rows)
	if db.Rdb != nil {
		if raw, err := json.Marshal(res); err == nil {
			_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), 10*time.Minute).Err()
		}
	}
	return res
}

func EnsureActiveFilterOptionSnapshot() error {
	version := GetActiveSnapshotVersion()
	if version == "" {
		return nil
	}

	return RebuildFilterOptionSnapshot(version)
}

func ClearFilterOptionSnapshotsTx(tx *gorm.DB, version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil
	}
	return tx.Where("snapshot_version = ?", version).Unscoped().Delete(&model.FilmFilterOptionSnapshot{}).Error
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
	version := GetActiveSnapshotVersion()
	if version == "" {
		return map[int64]map[string]any{}
	}
	var rows []model.FilmFilterOptionSnapshot
	if err := db.Mdb.Where("snapshot_version = ?", version).Order("pid ASC, sort ASC, id ASC").Find(&rows).Error; err != nil || len(rows) == 0 {
		return map[int64]map[string]any{}
	}
	groupedByPid := make(map[int64][]model.FilmFilterOptionSnapshot)
	for _, row := range rows {
		groupedByPid[row.Pid] = append(groupedByPid[row.Pid], row)
	}
	result := make(map[int64]map[string]any, len(groupedByPid))
	for pid, pidRows := range groupedByPid {
		resp := buildFilterOptionResponse(pidRows)
		if tags, _ := resp["tags"].(map[string]any); tags != nil {
			result[pid] = tags
		}
	}
	return result
}
