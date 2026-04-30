package film

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"server/internal/model"
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

func AppendSearchOption(options []map[string]string, option map[string]string) []map[string]string {
	if option == nil {
		return options
	}
	if strings.TrimSpace(option["Value"]) == "" && strings.TrimSpace(option["Name"]) != "全部" {
		return options
	}
	for _, item := range options {
		if item["Value"] == option["Value"] {
			return options
		}
	}
	return append(options, option)
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

func LimitSearchTagItems(items []model.SearchTagItem, limit int) []model.SearchTagItem {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
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

func isAbnormalSearchTagItem(tagType string, item model.SearchTagItem) bool {
	value := strings.TrimSpace(item.Value)
	name := strings.TrimSpace(item.Name)
	if value == "" || name == "" {
		return true
	}
	if isOthersSearchTagValue(value) || isOthersSearchTagValue(name) {
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

func isOthersSearchTagValue(value string) bool {
	switch strings.TrimSpace(value) {
	case model.TagOthersValue, model.TagOthersName, "其它":
		return true
	default:
		return false
	}
}

func SplitSearchTagItems(tagType string, items []model.SearchTagItem) ([]model.SearchTagItem, []model.SearchTagItem) {
	normalItems := make([]model.SearchTagItem, 0, len(items))
	abnormalItems := make([]model.SearchTagItem, 0)
	for _, item := range items {
		if isAbnormalSearchTagItem(tagType, item) {
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
