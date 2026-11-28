package enrich

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"server/internal/analytics/types"
)

// ResolveClassifyMemberMemoryOnly 在统计写入路径解析分类榜单成员 Key（零 SQL）
func ResolveClassifyMemberMemoryOnly(evt *types.Event) string {
	if evt == nil {
		return ""
	}
	if evt.Action == types.ActionClassify {
		res := strings.TrimSpace(evt.Resource)
		if res != "" && res != "list" && res != "config" {
			if _, ok := ParseFilmID(res); ok {
				return res
			}
			if catID, found := ResolveCategoryIDByNameMemoryOnly(res); found {
				return strconv.FormatInt(catID, 10)
			}
			return res
		}
		if trimmedCat := strings.TrimSpace(evt.ResourceCat); trimmedCat != "" {
			if catID, found := ResolveCategoryIDByNameMemoryOnly(trimmedCat); found {
				return strconv.FormatInt(catID, 10)
			}
			return trimmedCat
		}
		return ""
	}

	if evt.Action == types.ActionPlay || evt.PlayMember != "" {
		cat := strings.TrimSpace(evt.ResourceCat)
		if cat == "" && evt.PlayMember != "" {
			if id, ok := ParseFilmID(evt.PlayMember); ok {
				if meta, found := GetFilmMetaFromMemory(id); found && meta.Category != "" {
					cat = strings.TrimSpace(meta.Category)
				}
			}
		}
		if cat != "" {
			if catID, found := ResolveCategoryIDByNameMemoryOnly(cat); found {
				return strconv.FormatInt(catID, 10)
			}
			return cat
		}
	}
	return ""
}

// SnapshotEvent 在写入 Redis 前尝试使用内存缓存固化影片/分类元数据快照（零 SQL）
func SnapshotEvent(evt *types.Event) {
	if evt == nil {
		return
	}
	evt.ResourcePoster = SanitizePosterURL(evt.ResourcePoster)
	res := ExtractSingleResourceToken(evt.Resource)

	if IsPlayEvent(evt) {
		id, ok := ParseFilmID(res)
		if !ok {
			return
		}
		if evt.ResourceTitle != "" && !strings.HasPrefix(evt.ResourceTitle, "影片 #") &&
			evt.ResourceCat != "" && evt.ResourcePoster != "" {
			return
		}
		m, found := GetFilmMetaFromMemory(id)
		if !found {
			return
		}
		if evt.ResourceTitle == "" || strings.HasPrefix(evt.ResourceTitle, "影片 #") {
			if !strings.HasPrefix(m.Title, "影片 #") {
				evt.ResourceTitle = m.Title
			}
		}
		if evt.ResourcePoster == "" && m.Poster != "" {
			evt.ResourcePoster = SanitizePosterURL(m.Poster)
		}
		if evt.ResourceCat == "" && m.Category != "" {
			evt.ResourceCat = m.Category
		}
		return
	}

	if evt.Action != types.ActionClassify {
		return
	}
	if evt.ResourceCat != "" && !strings.HasPrefix(evt.ResourceCat, "分类 #") {
		if evt.ResourceTitle == "" {
			evt.ResourceTitle = evt.ResourceCat
		}
		return
	}
	id, ok := ParseFilmID(res)
	if !ok {
		return
	}
	name, found := GetCategoryNameFromMemory(id)
	if !found || name == "" || strings.HasPrefix(name, "分类 #") {
		return
	}
	evt.ResourceCat = name
	if evt.ResourceTitle == "" {
		evt.ResourceTitle = name
	}
}

// TakePlayTops 补齐热播榜片名/海报/分类并截断到 limit
func TakePlayTops(items []types.TopItem, limit int) ([]types.TopItem, error) {
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		if it.Title != "" && !strings.HasPrefix(it.Title, "影片 #") {
			continue
		}
		if id, ok := ParseFilmID(it.Key); ok {
			ids = append(ids, id)
		}
	}
	metaMap, err := ResolveFilmMetas(ids)
	if err != nil {
		return nil, err
	}
	valid := make([]types.TopItem, 0, len(items))
	for _, it := range items {
		id, ok := ParseFilmID(it.Key)
		if !ok {
			continue
		}
		it.Key = strconv.FormatInt(id, 10)
		if it.Title == "" || strings.HasPrefix(it.Title, "影片 #") {
			if meta, ok := metaMap[id]; ok {
				it.Title = meta.Title
				it.Category = meta.Category
				it.Poster = meta.Poster
				it.Year = meta.Year
			}
		}
		if it.Title == "" {
			continue
		}
		valid = append(valid, it)
	}
	return LimitTopItems(valid, limit), nil
}

// TakeClassifyTops 补齐分类榜名称、合并同名条目并截断到 limit
func TakeClassifyTops(items []types.TopItem, limit int) ([]types.TopItem, error) {
	ids := make([]int64, 0, len(items))
	names := make([]string, 0, len(items))
	for _, it := range items {
		trimmed := strings.TrimSpace(it.Key)
		if trimmed == "" || trimmed == "list" || trimmed == "config" {
			continue
		}
		if id, ok := ParseFilmID(trimmed); ok {
			if it.Category == "" || strings.HasPrefix(it.Category, "分类 #") {
				ids = append(ids, id)
			}
		} else {
			names = append(names, trimmed)
		}
	}
	navIdx := LoadCategoryNavIndex()
	catMap, err := ResolveCategoryNames(ids)
	if err != nil {
		return nil, err
	}
	nameToID, err := resolveCategoryIDsByNames(names)
	if err != nil {
		return nil, err
	}

	valid := make([]types.TopItem, 0, len(items))
	for _, it := range items {
		trimmed := strings.TrimSpace(it.Key)
		if trimmed == "" || trimmed == "list" || trimmed == "config" {
			continue
		}
		if id, ok := ParseFilmID(trimmed); ok {
			it.Key = strconv.FormatInt(id, 10)
			if target, hit := navIdx.LookupByID(id); hit {
				it.Key = strconv.FormatInt(target.ID, 10)
				if target.Name != "" {
					it.Category = target.Name
					it.Title = target.Name
				}
				it.Href = target.Href
			} else {
				if it.Category == "" || strings.HasPrefix(it.Category, "分类 #") {
					if name, ok := catMap[id]; ok && name != "" {
						it.Category = name
					}
				}
				if it.Title == "" || strings.HasPrefix(it.Title, "分类 #") {
					if it.Category != "" {
						it.Title = it.Category
					} else {
						it.Title = fmt.Sprintf("分类 #%d", id)
					}
				}
				// 非主站或旧分类 ID，若当前主站存在同名可见分类，则对齐至主站有效导航链接
				if target, hit := navIdx.LookupByName(it.Title); hit {
					it.Key = strconv.FormatInt(target.ID, 10)
					it.Category = target.Name
					it.Title = target.Name
					it.Href = target.Href
				}
			}
		} else {
			if target, hit := navIdx.LookupByName(trimmed); hit {
				it.Key = strconv.FormatInt(target.ID, 10)
				it.Category = target.Name
				it.Title = target.Name
				it.Href = target.Href
			} else if catID, ok := nameToID[trimmed]; ok {
				it.Key = strconv.FormatInt(catID, 10)
				it.Category = trimmed
				it.Title = trimmed
			} else {
				it.Key = trimmed
				if it.Category == "" {
					it.Category = trimmed
				}
				if it.Title == "" {
					it.Title = trimmed
				}
			}
		}
		valid = append(valid, it)
	}
	return LimitTopItems(mergeClassifyItems(valid), limit), nil
}

// EnrichLogEvents 批量为访问流水补齐影片与分类元数据
func EnrichLogEvents(events []types.Event) ([]types.Event, error) {
	if len(events) == 0 {
		return events, nil
	}
	filmIDs := make([]int64, 0, len(events))
	catIDs := make([]int64, 0, len(events))

	for i := range events {
		it := &events[i]
		res := ExtractSingleResourceToken(it.Resource)
		if IsPlayEvent(it) {
			if it.ResourceTitle != "" && !strings.HasPrefix(it.ResourceTitle, "影片 #") {
				continue
			}
			if id, ok := ParseFilmID(res); ok {
				filmIDs = append(filmIDs, id)
			}
		} else if it.Action == types.ActionClassify {
			if it.ResourceCat != "" && !strings.HasPrefix(it.ResourceCat, "分类 #") {
				continue
			}
			if id, ok := ParseFilmID(res); ok {
				catIDs = append(catIDs, id)
			}
		}
	}

	metaMap, err := ResolveFilmMetas(filmIDs)
	if err != nil {
		return nil, err
	}
	catMap, err := ResolveCategoryNames(catIDs)
	if err != nil {
		return nil, err
	}
	var navIdx *CategoryNavIndex
	for i := range events {
		if events[i].Action == types.ActionClassify {
			navIdx = LoadCategoryNavIndex()
			break
		}
	}

	for i := range events {
		it := &events[i]
		res := ExtractSingleResourceToken(it.Resource)
		if IsPlayEvent(it) {
			if it.ResourceTitle == "" || strings.HasPrefix(it.ResourceTitle, "影片 #") {
				if id, ok := ParseFilmID(res); ok {
					if meta, ok := metaMap[id]; ok {
						it.ResourceTitle = meta.Title
						it.ResourcePoster = meta.Poster
						it.ResourceCat = meta.Category
					}
				}
			}
		} else if it.Action == types.ActionClassify {
			if id, ok := ParseFilmID(res); ok {
				if target, hit := navIdx.LookupByID(id); hit {
					if target.Name != "" {
						it.ResourceCat = target.Name
					}
					it.ResourceHref = target.Href
				} else {
					if it.ResourceCat == "" || strings.HasPrefix(it.ResourceCat, "分类 #") {
						if name, ok := catMap[id]; ok && name != "" {
							it.ResourceCat = name
						} else if it.ResourceCat == "" {
							it.ResourceCat = fmt.Sprintf("分类 #%d", id)
						}
					}
					if target, hit := navIdx.LookupByName(it.ResourceCat); hit {
						it.ResourceHref = target.Href
					}
				}
			} else if target, hit := navIdx.LookupByName(firstNonEmptyString(it.ResourceCat, res)); hit {
				it.ResourceCat = target.Name
				it.ResourceHref = target.Href
			}
		}
	}
	return events, nil
}

func firstNonEmptyString(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// LimitTopItems 截断榜单切片至上限
func LimitTopItems(items []types.TopItem, limit int) []types.TopItem {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func mergeClassifyItems(items []types.TopItem) []types.TopItem {
	if len(items) <= 1 {
		return items
	}
	merged := make([]types.TopItem, 0, len(items))
	idxMap := make(map[string]int, len(items))
	for _, it := range items {
		lookupKey := it.Key
		if it.Title != "" {
			lookupKey = it.Title
		}
		if idx, exists := idxMap[lookupKey]; exists {
			merged[idx].Count += it.Count
			if merged[idx].Href == "" && it.Href != "" {
				merged[idx].Href = it.Href
				merged[idx].Key = it.Key
			}
		} else {
			idxMap[lookupKey] = len(merged)
			merged = append(merged, it)
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].Count > merged[j].Count
	})
	return merged
}
