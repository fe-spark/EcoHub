package film

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

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
			list = append(list, map[string]string{"Name": sl[0], "Value": sl[1]})
		}
	}

	return list
}

func AppendSearchOption(options []map[string]string, option map[string]string) []map[string]string {
	if option == nil {
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
			return append(displayItems, item)
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

func ParseSearchTagYear(value string) (int, bool) {
	year, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || year <= 0 {
		return 0, false
	}
	return year, true
}

func FormatSearchTagItems(tagType string, items []model.SearchTagItem, sticky string) []map[string]string {
	if strings.EqualFold(tagType, "Year") {
		items = SortYearSearchTagItems(items)
	}

	topCount := SearchTagDisplayLimit
	if len(items) < topCount {
		topCount = len(items)
	}
	displayItems := AppendStickySearchTag(items, sticky, topCount)
	hasMore := len(items) > SearchTagDisplayLimit

	tagStrs := make([]string, 0, len(displayItems))
	for _, item := range displayItems {
		tagStrs = append(tagStrs, fmt.Sprintf("%s:%s", item.Name, item.Value))
	}

	formatted := HandleTagStr(tagType, true, tagStrs...)
	if hasMore {
		formatted = append(formatted, map[string]string{"Name": model.TagOthersName, "Value": model.TagOthersValue})
	}
	return formatted
}
