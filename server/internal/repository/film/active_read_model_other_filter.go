package film

import (
	"log"
	"strconv"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"

	"gorm.io/gorm"
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

// loadFilterOptionTags 取当前一级分类的筛选项 tags，与前台展示同一份数据（含 Redis 缓存）。
// 「其他」的可见取值集合必须以此为准：展示列表之外的取值才算其他。
func loadFilterOptionTags(version string, pid int64) map[string]any {
	options := GetFilterOptionSnapshot(version, pid)
	if options == nil {
		return nil
	}
	tags, ok := options["tags"].(map[string]any)
	if !ok {
		return nil
	}
	return tags
}

func visibleSearchTagStrings(tags map[string]any, tagType string) []string {
	visibleValues := getVisibleSearchTagValues(tags[tagType])
	if len(visibleValues) == 0 {
		return nil
	}
	res := make([]string, 0, len(visibleValues))
	for v := range visibleValues {
		res = append(res, v)
	}
	return res
}

func visibleSearchTagYears(tags map[string]any) []int64 {
	strs := visibleSearchTagStrings(tags, "Year")
	if len(strs) == 0 {
		return nil
	}
	years := make([]int64, 0, len(strs))
	for _, s := range strs {
		if y, err := strconv.ParseInt(s, 10, 64); err == nil && y > 0 {
			years = append(years, y)
		}
	}
	return years
}

// logOthersFilterFallback 筛选项缺失时算不出可见集合，只能退化为空结果，留日志便于排查。
func logOthersFilterFallback(version string, pid int64, tagType string) {
	log.Printf("[FilmClassifySearch] 筛选项缺失，%s=其他 无法计算可见集合，按空结果处理 pid=%d version=%s", tagType, pid, version)
}

// applyTagSearchFilter 应用剧情/地区/语言/年份筛选。
// 「其他」= 该维度取值不在前台展示的筛选项里（空值与异常值同样算其他），
// 可见集合取当前分类的筛选项快照，同一次请求只取一次。
func applyTagSearchFilter(query *gorm.DB, version string, st model.SearchTagsVO) *gorm.DB {
	var tags map[string]any
	if st.Plot == model.TagOthersValue || st.Area == model.TagOthersValue ||
		st.Language == model.TagOthersValue || st.Year == model.TagOthersValue {
		tags = loadFilterOptionTags(version, st.Pid)
	}
	if st.Plot != "" && st.Plot != "全部" && st.Plot != model.TagUnknownValue {
		if st.Plot == model.TagOthersValue {
			// AND 语义：class_tag 为空、或不含任何可见剧情标签时算其他。
			// class_tag 是多值文本，这里用 NOT LIKE 近似判定，可见标签互为子串时可能误判。
			visiblePlots := visibleSearchTagStrings(tags, "Plot")
			if len(visiblePlots) > 0 {
				cond := db.Mdb.Where("class_tag = '' OR class_tag IS NULL")
				allNotLike := db.Mdb
				for _, vp := range visiblePlots {
					allNotLike = allNotLike.Where("class_tag NOT LIKE ?", "%"+escapeLikePattern(vp)+"%")
				}
				query = query.Where(cond.Or(allNotLike))
			} else {
				logOthersFilterFallback(version, st.Pid, "Plot")
				query = query.Where("class_tag = ?", model.TagOthersValue)
			}
		} else {
			query = query.Where("class_tag LIKE ?", "%"+escapeLikePattern(st.Plot)+"%")
		}
	}

	if st.Area != "" && st.Area != "全部" && st.Area != model.TagUnknownValue {
		if st.Area == model.TagOthersValue {
			visibleAreas := visibleSearchTagStrings(tags, "Area")
			if len(visibleAreas) > 0 {
				query = query.Where("area NOT IN ? OR area = '' OR area IS NULL", visibleAreas)
			} else {
				logOthersFilterFallback(version, st.Pid, "Area")
				query = query.Where("area = ?", model.TagOthersValue)
			}
		} else {
			query = query.Where("area = ?", st.Area)
		}
	}

	if st.Language != "" && st.Language != "全部" && st.Language != model.TagUnknownValue {
		if st.Language == model.TagOthersValue {
			visibleLangs := visibleSearchTagStrings(tags, "Language")
			if len(visibleLangs) > 0 {
				query = query.Where("language NOT IN ? OR language = '' OR language IS NULL", visibleLangs)
			} else {
				logOthersFilterFallback(version, st.Pid, "Language")
				query = query.Where("language = ?", model.TagOthersValue)
			}
		} else {
			query = query.Where("language = ?", st.Language)
		}
	}

	if st.Year != "" && st.Year != "全部" && st.Year != model.TagUnknownValue {
		if st.Year == model.TagOthersValue {
			visibleYears := visibleSearchTagYears(tags)
			if len(visibleYears) > 0 {
				query = query.Where("year NOT IN ? OR year <= 0 OR year IS NULL", visibleYears)
			} else {
				logOthersFilterFallback(version, st.Pid, "Year")
				query = query.Where("year = -1")
			}
		} else {
			if yearInt, err := strconv.ParseInt(st.Year, 10, 64); err == nil && yearInt > 0 {
				query = query.Where("year = ?", yearInt)
			}
		}
	}

	return query
}
