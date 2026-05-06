package film

import (
	"fmt"
	"strings"

	"server/internal/model"
)

func loadSearchTagItemsByTypeFromReadModel(version string, pid int64) map[string][]model.SearchTagItem {
	itemsByType := make(map[string][]model.SearchTagItem)
	readModel := GetActiveFilmReadModel()
	if readModel == nil || readModel.Version != version || pid <= 0 {
		return itemsByType
	}
	return searchTagItemsByTypeFromSnapshots(readModel.projectedSnapshotsByPid(pid))
}

func loadSearchTagItemsByTypeFromProjectedReadModel(pid int64, projected *ProjectedFilmReadModel) map[string][]model.SearchTagItem {
	itemsByType := make(map[string][]model.SearchTagItem)
	if projected == nil || pid <= 0 {
		return itemsByType
	}
	snapshots := make([]model.FilmListSnapshot, 0, len(projected.ByPid[pid]))
	for _, mid := range projected.ByPid[pid] {
		if snapshot, ok := projected.ByMid[mid]; ok {
			snapshots = append(snapshots, snapshot)
		}
	}
	return searchTagItemsByTypeFromSnapshots(snapshots)
}

func searchTagItemsByTypeFromSnapshots(snapshots []model.FilmListSnapshot) map[string][]model.SearchTagItem {
	areaCounts := make(map[string]int64)
	languageCounts := make(map[string]int64)
	yearCounts := make(map[string]int64)
	plotCounts := make(map[string]int64)
	for _, snapshot := range snapshots {
		countSingleSearchTag("Area", snapshot.Area, areaCounts)
		countSingleSearchTag("Language", snapshot.Language, languageCounts)
		if snapshot.Year > 0 {
			yearCounts[fmt.Sprint(snapshot.Year)]++
		} else {
			yearCounts[model.TagUnknownValue]++
		}
		countPlotSearchTags(snapshot.ClassTag, plotCounts)
	}
	return map[string][]model.SearchTagItem{
		"Area":     searchTagItemsFromCounts("Area", areaCounts),
		"Language": searchTagItemsFromCounts("Language", languageCounts),
		"Year":     searchTagItemsFromCounts("Year", yearCounts),
		"Plot":     searchTagItemsFromCounts("Plot", plotCounts),
	}
}

func countSingleSearchTag(tagType string, raw string, counts map[string]int64) {
	if value := normalizeSearchTagValue(tagType, raw); value != "" {
		counts[value]++
		return
	}
	counts[model.TagUnknownValue]++
}

func countPlotSearchTags(classTag string, counts map[string]int64) {
	hasValidTag := false
	for _, tag := range splitClassTags(classTag) {
		if value := normalizeSearchTagValue("Plot", tag); value != "" {
			counts[value]++
			hasValidTag = true
		}
	}
	if !hasValidTag {
		counts[model.TagUnknownValue]++
	}
}

func searchTagItemsFromCounts(tagType string, counts map[string]int64) []model.SearchTagItem {
	items := make([]model.SearchTagItem, 0, len(counts))
	for value, score := range counts {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		name := value
		if value == model.TagUnknownValue {
			name = model.TagUnknownName
		}
		items = append(items, model.SearchTagItem{TagType: tagType, Name: name, Value: value, Score: score})
	}
	return SortSearchTagItems(tagType, items)
}
