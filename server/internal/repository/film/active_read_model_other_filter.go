package film

import (
	"strconv"
	"strings"

	"server/internal/model"
	"server/internal/repository/support"
)

func buildOtherReadModelFilters(st model.SearchTagsVO) []string {
	filters := make([]string, 0, 4)
	if strings.TrimSpace(st.Plot) == model.TagOthersValue {
		filters = append(filters, "Plot")
	}
	if strings.TrimSpace(st.Area) == model.TagOthersValue {
		filters = append(filters, "Area")
	}
	if strings.TrimSpace(st.Language) == model.TagOthersValue {
		filters = append(filters, "Language")
	}
	if strings.TrimSpace(st.Year) == model.TagOthersValue {
		filters = append(filters, "Year")
	}
	return filters
}

func clearOtherReadModelFilters(st model.SearchTagsVO) model.SearchTagsVO {
	if strings.TrimSpace(st.Plot) == model.TagOthersValue {
		st.Plot = ""
	}
	if strings.TrimSpace(st.Area) == model.TagOthersValue {
		st.Area = ""
	}
	if strings.TrimSpace(st.Language) == model.TagOthersValue {
		st.Language = ""
	}
	if strings.TrimSpace(st.Year) == model.TagOthersValue {
		st.Year = ""
	}
	return st
}

func filterOtherSearchTagSnapshots(snapshots []model.FilmListSnapshot, st model.SearchTagsVO, tagTypes []string, options map[int64]map[string]any) []model.FilmListSnapshot {
	if len(tagTypes) == 0 {
		return snapshots
	}
	visibleValuesByType := buildVisibleSearchTagValues(st.Pid, tagTypes, options)
	filtered := make([]model.FilmListSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		matched := true
		for _, tagType := range tagTypes {
			if !snapshotMatchesOtherSearchTag(snapshot, tagType, visibleValuesByType[tagType]) {
				matched = false
				break
			}
		}
		if matched {
			filtered = append(filtered, snapshot)
		}
	}
	return filtered
}

func buildVisibleSearchTagValues(pid int64, tagTypes []string, options map[int64]map[string]any) map[string]map[string]struct{} {
	result := make(map[string]map[string]struct{}, len(tagTypes))
	optionTags := getProjectedFilterOptionTags(pid, options)
	for _, tagType := range tagTypes {
		result[tagType] = getVisibleSearchTagValues(optionTags[tagType])
	}
	return result
}

func getProjectedFilterOptionTags(pid int64, options map[int64]map[string]any) map[string]any {
	response := options[support.ResolveCategoryID(pid)]
	if response == nil {
		return map[string]any{}
	}
	tags, _ := response["tags"].(map[string]any)
	if tags == nil {
		return map[string]any{}
	}
	return tags
}

func getVisibleSearchTagValues(raw any) map[string]struct{} {
	values := make(map[string]struct{})
	switch list := raw.(type) {
	case []map[string]string:
		for _, item := range list {
			appendVisibleSearchTagValue(values, item["Value"])
		}
	case []any:
		for _, rawItem := range list {
			item, ok := rawItem.(map[string]any)
			if !ok {
				continue
			}
			value, _ := item["Value"].(string)
			appendVisibleSearchTagValue(values, value)
		}
	}
	return values
}

func appendVisibleSearchTagValue(values map[string]struct{}, value string) {
	value = strings.TrimSpace(value)
	if value == "" || value == model.TagOthersValue || value == model.TagUnknownValue {
		return
	}
	values[value] = struct{}{}
}

func snapshotMatchesOtherSearchTag(snapshot model.FilmListSnapshot, tagType string, visibleValues map[string]struct{}) bool {
	switch tagType {
	case "Plot":
		tags := splitClassTags(snapshot.ClassTag)
		if len(tags) == 0 {
			return true
		}
		for _, tag := range tags {
			item := model.SearchTagItem{TagType: tagType, Name: tag, Value: tag}
			if searchTagValueIsOther(tagType, item, visibleValues) {
				return true
			}
		}
		return false
	case "Year":
		if snapshot.Year <= 0 {
			return true
		}
		value := strconv.FormatInt(snapshot.Year, 10)
		return searchTagValueIsOther(tagType, model.SearchTagItem{TagType: tagType, Name: value, Value: value}, visibleValues)
	case "Area":
		return searchTagValueIsOther(tagType, model.SearchTagItem{TagType: tagType, Name: snapshot.Area, Value: snapshot.Area}, visibleValues)
	case "Language":
		return searchTagValueIsOther(tagType, model.SearchTagItem{TagType: tagType, Name: snapshot.Language, Value: snapshot.Language}, visibleValues)
	default:
		return false
	}
}

func searchTagValueIsOther(tagType string, item model.SearchTagItem, visibleValues map[string]struct{}) bool {
	item.Value = normalizeSearchTagValue(tagType, item.Value)
	item.Name = normalizeSearchTagValue(tagType, item.Name)
	if isAbnormalSearchTagItem(tagType, item) || item.Value == model.TagUnknownValue {
		return true
	}
	_, ok := visibleValues[item.Value]
	return !ok
}
