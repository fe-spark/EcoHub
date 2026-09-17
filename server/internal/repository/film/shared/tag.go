package shared

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"server/internal/model"
	"server/internal/repository/support"
)

const SearchTagDisplayLimit = 12

func HandleTagStr(title string, withAll bool, tags ...string) []map[string]string {
	list := make([]map[string]string, 0)

	if withAll && !strings.EqualFold(title, "Sort") {
		list = append(list, map[string]string{"Name": "全部", "Value": ""})
	}

	for _, t := range tags {
		if sl := strings.Split(t, ":"); len(sl) > 1 {
			if strings.TrimSpace(sl[0]) == "" || strings.TrimSpace(sl[1]) == "" {
				continue
			}
			list = append(list, map[string]string{"Name": sl[0], "Value": sl[1]})
		}
	}

	return list
}

func AppendStickySearchTag(items []model.SearchTagItem, sticky string, topCount int) []model.SearchTagItem {
	if sticky == "" || sticky == model.TagOthersValue || len(items) <= topCount {
		if topCount > len(items) {
			topCount = len(items)
		}
		return items[:topCount]
	}

	displayItems := items[:topCount]
	for _, item := range displayItems {
		if item.Value == sticky {
			return displayItems
		}
	}
	for _, item := range items[topCount:] {
		if item.Value == sticky {
			if len(displayItems) == 0 {
				return []model.SearchTagItem{item}
			}
			displayItems[len(displayItems)-1] = item
			return displayItems
		}
	}
	return displayItems
}

func SortYearSearchTagItems(items []model.SearchTagItem) []model.SearchTagItem {
	if len(items) < 2 {
		return items
	}

	sorted := append([]model.SearchTagItem(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool {
		leftYear, leftOK := ParseSearchTagYear(sorted[i].Value)
		rightYear, rightOK := ParseSearchTagYear(sorted[j].Value)

		switch {
		case leftOK && rightOK:
			return leftYear > rightYear
		case leftOK:
			return true
		case rightOK:
			return false
		default:
			return sorted[i].Score > sorted[j].Score
		}
	})
	return sorted
}

func SortSearchTagItems(tagType string, items []model.SearchTagItem) []model.SearchTagItem {
	if strings.EqualFold(tagType, "Year") {
		return SortYearSearchTagItems(items)
	}
	if len(items) < 2 {
		return items
	}

	sorted := append([]model.SearchTagItem(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Score != sorted[j].Score {
			return sorted[i].Score > sorted[j].Score
		}
		return sorted[i].Value < sorted[j].Value
	})
	return sorted
}

func ParseSearchTagYear(value string) (int, bool) {
	year, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || year <= 0 {
		return 0, false
	}
	return year, true
}

func IsAbnormalSearchTagItem(tagType string, item model.SearchTagItem) bool {
	value := strings.TrimSpace(item.Value)
	name := strings.TrimSpace(item.Name)
	if value == "" || name == "" {
		return true
	}
	if IsOthersSearchTagValue(value) || IsOthersSearchTagValue(name) {
		return true
	}

	switch {
	case strings.EqualFold(tagType, "Year"):
		_, ok := ParseSearchTagYear(value)
		return !ok
	case strings.EqualFold(tagType, "Area"):
		return isAbnormalTextTagValue(value, 2, 8)
	case strings.EqualFold(tagType, "Language"):
		return isAbnormalTextTagValue(value, 1, 10)
	case strings.EqualFold(tagType, "Plot"):
		return isAbnormalTextTagValue(value, 2, 8)
	default:
		return false
	}
}

func isAbnormalTextTagValue(value string, minLen int, maxLen int) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}

	runes := []rune(value)
	if len(runes) < minLen || len(runes) > maxLen {
		return true
	}

	for _, r := range runes {
		if unicode.IsDigit(r) {
			return true
		}
		if r <= unicode.MaxASCII && unicode.IsLetter(r) {
			return true
		}
		if strings.ContainsRune(",|/\\_+&.=()[]{}<>-", r) {
			return true
		}
	}

	return false
}

func IsOthersSearchTagValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case model.TagOthersValue, model.TagOthersName, "其它", "others", "other":
		return true
	default:
		return false
	}
}

func SplitSearchTagItems(tagType string, items []model.SearchTagItem) ([]model.SearchTagItem, []model.SearchTagItem) {
	normalItems := make([]model.SearchTagItem, 0, len(items))
	abnormalItems := make([]model.SearchTagItem, 0)
	for _, item := range items {
		if IsAbnormalSearchTagItem(tagType, item) {
			abnormalItems = append(abnormalItems, item)
			continue
		}
		normalItems = append(normalItems, item)
	}
	return normalItems, abnormalItems
}

func FormatSearchTagItems(tagType string, items []model.SearchTagItem, sticky string, includeOthers bool) []map[string]string {
	return formatSearchTagItems(tagType, items, sticky, includeOthers, SearchTagDisplayLimit)
}

func formatSearchTagItems(tagType string, items []model.SearchTagItem, sticky string, includeOthers bool, limit int) []map[string]string {
	normalItems, abnormalItems := SplitSearchTagItems(tagType, items)
	items = SortSearchTagItems(tagType, normalItems)

	displayItems := items
	if limit > 0 {
		topCount := limit
		if len(items) < topCount {
			topCount = len(items)
		}
		displayItems = AppendStickySearchTag(items, sticky, topCount)
	}
	hasMore := limit > 0 && len(items) > limit
	hasOthers := hasMore || len(abnormalItems) > 0 || includeOthers

	tagStrs := make([]string, 0, len(displayItems))
	for _, item := range displayItems {
		if strings.TrimSpace(item.Value) == "" && strings.TrimSpace(item.Name) != "全部" {
			continue
		}
		tagStrs = append(tagStrs, fmt.Sprintf("%s:%s", item.Name, item.Value))
	}

	formatted := HandleTagStr(tagType, true, tagStrs...)
	if hasOthers {
		formatted = append(formatted, map[string]string{"Name": model.TagOthersName, "Value": model.TagOthersValue})
	}
	return formatted
}

// DefaultSortTagStrings 分类无自定义排序标签时的兜底排序选项。
var DefaultSortTagStrings = []string{"最近更新:update_stamp", "人气:hits", "评分:score", "时间:year"}

var (
	tagWhitespaceRe = regexp.MustCompile(`[\s\n\r]+`)
	tagSeparatorRe  = regexp.MustCompile(`[/,，、\s\.\+\|]`)
)

// SplitRawSearchTags 归一化原始标签串（去空白）并按分隔符拆分为取值列表。
func SplitRawSearchTags(raw string) []string {
	return tagSeparatorRe.Split(tagWhitespaceRe.ReplaceAllString(raw, ""), -1)
}

// NormalizeSearchTagValue 归一化搜索标签取值：去除首尾空白与多余冒号，过滤「地区」「语言」这类无意义表头值。
func NormalizeSearchTagValue(tagType string, value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimRight(value, ":：")

	switch tagType {
	case "Area":
		switch value {
		case "地区", "制片国家", "制片国家地区":
			return ""
		}
	case "Language":
		switch value {
		case "语言", "对白语言":
			return ""
		}
	}

	return value
}

// NormalizeSearchTagsVO 归一化检索条件：分类 ID 归位，并把「其它」类取值统一为 TagOthersValue。
func NormalizeSearchTagsVO(st model.SearchTagsVO) model.SearchTagsVO {
	st.Pid = support.ResolveCategoryID(st.Pid)
	if st.Cid > 0 {
		st.Cid = support.ResolveCategoryID(st.Cid)
	}
	if IsOthersSearchTagValue(st.Plot) {
		st.Plot = model.TagOthersValue
	}
	if IsOthersSearchTagValue(st.Area) {
		st.Area = model.TagOthersValue
	}
	if IsOthersSearchTagValue(st.Language) {
		st.Language = model.TagOthersValue
	}
	if IsOthersSearchTagValue(st.Year) {
		st.Year = model.TagOthersValue
	}
	return st
}
