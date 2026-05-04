package film

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

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
	// 筛选项不联动：选项统计固定在当前一级分类内；列表查询由 ActiveReadModel/倒排索引完整应用筛选条件。
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
