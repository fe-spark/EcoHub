package snapshot

import (
	"sort"
	"strconv"
	"strings"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	"server/internal/repository/film/shared"
	"server/internal/repository/support"
)

// tagPageFromIndex 分类筛选在内存索引上计数并取一页。只按主键回表取当前页。
func tagPageFromIndex(version string, st model.SearchTagsVO, page *dto.Page) (tagSearchCacheItem, error) {
	if st.Cid > 0 && !support.IsCategoryVisible(st.Cid, st.Pid) {
		return tagSearchCacheItem{Total: 0, PageCount: 1, Snapshots: []model.FilmListSnapshot{}}, nil
	}
	if st.Pid > 0 && !support.IsCategoryVisible(0, st.Pid) {
		return tagSearchCacheItem{Total: 0, PageCount: 1, Snapshots: []model.FilmListSnapshot{}}, nil
	}
	idx, err := loadFilmSearchMetaIndex(version)
	if err != nil {
		return tagSearchCacheItem{}, err
	}
	match, err := tagMetaMatch(version, st)
	if err != nil {
		return tagSearchCacheItem{}, err
	}
	limit := page.PageSize
	if limit <= 0 {
		limit = 20
	}
	positions, bounded := categoryPositions(idx, st)
	mids, total := pageTagMids(idx, match, st.Sort, shared.PageOffset(page), limit, positions, bounded)
	snaps, err := GetSnapshotsByMidsOrdered("", mids)
	if err != nil {
		return tagSearchCacheItem{}, err
	}
	if snaps == nil {
		snaps = []model.FilmListSnapshot{}
	}
	pageCount := 1
	if total > 0 {
		pageCount = (total + limit - 1) / limit
	}
	return tagSearchCacheItem{Total: total, PageCount: pageCount, Snapshots: snaps}, nil
}

func tagMetaMatch(version string, st model.SearchTagsVO) (func(*FilmSearchMeta) bool, error) {
	sourceID := strings.TrimSpace(st.SourceId)
	keys := map[string]struct{}{}
	if sourceID != "" {
		field := "pid"
		id := st.Pid
		if st.Cid > 0 {
			field = "cid"
			id = st.Cid
		}
		for _, typeID := range repository.PublicSourceTypeIDs(sourceID, field, id) {
			key := support.BuildSourceCategoryKey(sourceID, typeID)
			if key != "" {
				keys[key] = struct{}{}
			}
		}
	}
	valueMatch, err := tagValueMatch(version, st)
	if err != nil {
		return nil, err
	}
	return func(item *FilmSearchMeta) bool {
		if item == nil {
			return false
		}
		if !support.IsCategoryVisible(item.Cid, item.Pid) {
			return false
		}
		if sourceID != "" {
			if _, ok := keys[item.CategoryKey]; !ok || !item.HasPlaySource(sourceID) {
				return false
			}
		} else if st.Cid > 0 {
			if item.Cid != st.Cid {
				return false
			}
		} else if st.Pid > 0 {
			if item.Pid != st.Pid {
				return false
			}
		}
		return valueMatch(item)
	}, nil
}

func tagValueMatch(version string, st model.SearchTagsVO) (func(*FilmSearchMeta) bool, error) {
	var tags map[string]any
	if st.Plot == model.TagOthersValue || st.Area == model.TagOthersValue ||
		st.Language == model.TagOthersValue || st.Year == model.TagOthersValue {
		var err error
		tags, err = loadFilterOptionTags(version, st.Pid, st.SourceId)
		if err != nil {
			return nil, err
		}
	}
	plot := activeTagValue(st.Plot)
	area := activeTagValue(st.Area)
	language := activeTagValue(st.Language)
	year := activeTagValue(st.Year)
	var visiblePlots, visibleAreas, visibleLangs map[string]struct{}
	var visibleYears map[int64]struct{}
	if plot == model.TagOthersValue {
		visiblePlots = tagSet(visibleSearchTagStrings(tags, "Plot"))
		if len(visiblePlots) == 0 {
			return nil, missingVisibleTags("剧情")
		}
	}
	if area == model.TagOthersValue {
		visibleAreas = tagSet(visibleSearchTagStrings(tags, "Area"))
		if len(visibleAreas) == 0 {
			return nil, missingVisibleTags("地区")
		}
	}
	if language == model.TagOthersValue {
		visibleLangs = tagSet(visibleSearchTagStrings(tags, "Language"))
		if len(visibleLangs) == 0 {
			return nil, missingVisibleTags("语言")
		}
	}
	if year == model.TagOthersValue {
		visibleYears = map[int64]struct{}{}
		for _, y := range visibleSearchTagYears(tags) {
			visibleYears[y] = struct{}{}
		}
		if len(visibleYears) == 0 {
			return nil, missingVisibleTags("年份")
		}
	}
	var yearInt int64
	if year != "" && year != model.TagOthersValue {
		parsed, err := strconv.ParseInt(year, 10, 64)
		if err != nil || parsed <= 0 {
			year = ""
		} else {
			yearInt = parsed
		}
	}
	return func(item *FilmSearchMeta) bool {
		if plot == model.TagOthersValue {
			if classTagHitsAny(item.ClassTag, visiblePlots) {
				return false
			}
		} else if plot != "" && !strings.Contains(item.ClassTag, plot) {
			return false
		}
		if area == model.TagOthersValue {
			if item.Area != "" {
				if _, ok := visibleAreas[item.Area]; ok {
					return false
				}
			}
		} else if area != "" && item.Area != area {
			return false
		}
		if language == model.TagOthersValue {
			if item.Language != "" {
				if _, ok := visibleLangs[item.Language]; ok {
					return false
				}
			}
		} else if language != "" && item.Language != language {
			return false
		}
		if year == model.TagOthersValue {
			if item.Item.Year > 0 {
				if _, ok := visibleYears[item.Item.Year]; ok {
					return false
				}
			}
		} else if year != "" && item.Item.Year != yearInt {
			return false
		}
		return true
	}, nil
}

func activeTagValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "全部" || value == model.TagUnknownValue {
		return ""
	}
	return value
}

func tagSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func classTagHitsAny(classTag string, visible map[string]struct{}) bool {
	if classTag == "" || len(visible) == 0 {
		return false
	}
	for value := range visible {
		if value != "" && strings.Contains(classTag, value) {
			return true
		}
	}
	return false
}

func categoryPositions(idx *filmSearchMetaIndex, st model.SearchTagsVO) ([]int, bool) {
	sourceID := strings.TrimSpace(st.SourceId)
	if sourceID == "" || idx == nil || len(idx.ByCategory) == 0 {
		return nil, false
	}
	field := "pid"
	id := st.Pid
	if st.Cid > 0 {
		field = "cid"
		id = st.Cid
	}
	positions := make([]int, 0)
	for _, typeID := range repository.PublicSourceTypeIDs(sourceID, field, id) {
		key := support.BuildSourceCategoryKey(sourceID, typeID)
		positions = append(positions, idx.ByCategory[key]...)
	}
	return positions, true
}

func pageTagMids(idx *filmSearchMetaIndex, match func(*FilmSearchMeta) bool, sortField string, offset, limit int, positions []int, bounded bool) ([]int64, int) {
	if idx == nil || match == nil {
		return []int64{}, 0
	}
	if offset < 0 {
		offset = 0
	}
	if !bounded && sortField != "hits" && sortField != "score" && sortField != "year" {
		total := 0
		mids := make([]int64, 0, limit)
		for _, pos := range idx.ByUpdate {
			if pos < 0 || pos >= len(idx.Items) || !match(&idx.Items[pos]) {
				continue
			}
			if total >= offset && len(mids) < limit {
				mids = append(mids, idx.Items[pos].Mid)
			}
			total++
		}
		return mids, total
	}
	iter := positions
	if !bounded {
		iter = make([]int, len(idx.Items))
		for i := range idx.Items {
			iter[i] = i
		}
	}
	cands := make([]int, 0)
	for _, pos := range iter {
		if pos < 0 || pos >= len(idx.Items) || !match(&idx.Items[pos]) {
			continue
		}
		cands = append(cands, pos)
	}
	sort.SliceStable(cands, func(i, j int) bool {
		return tagRankBefore(sortField, &idx.Items[cands[i]], &idx.Items[cands[j]])
	})
	return midsFromPositions(idx, cands, offset, limit), len(cands)
}

func tagRankBefore(sortField string, a, b *FilmSearchMeta) bool {
	switch sortField {
	case "hits":
		if a.Item.Hits != b.Item.Hits {
			return a.Item.Hits > b.Item.Hits
		}
	case "score":
		if a.Item.Score != b.Item.Score {
			return a.Item.Score > b.Item.Score
		}
	case "year":
		if a.Item.Year != b.Item.Year {
			return a.Item.Year > b.Item.Year
		}
		if a.Item.UpdateStamp != b.Item.UpdateStamp {
			return a.Item.UpdateStamp > b.Item.UpdateStamp
		}
	default:
		if a.Item.UpdateStamp != b.Item.UpdateStamp {
			return a.Item.UpdateStamp > b.Item.UpdateStamp
		}
	}
	return a.Mid > b.Mid
}

func midsFromPositions(idx *filmSearchMetaIndex, positions []int, offset, limit int) []int64 {
	if offset >= len(positions) || limit <= 0 {
		return []int64{}
	}
	end := offset + limit
	if end > len(positions) {
		end = len(positions)
	}
	mids := make([]int64, 0, end-offset)
	for _, pos := range positions[offset:end] {
		if pos < 0 || pos >= len(idx.Items) {
			continue
		}
		mids = append(mids, idx.Items[pos].Mid)
	}
	return mids
}

func countSourceTypeTagsFromIndex(version string, keys []string) (map[string]map[string]int, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	idx, err := loadFilmSearchMetaIndex(version)
	if err != nil {
		return nil, err
	}
	allow := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if key != "" {
			allow[key] = struct{}{}
		}
	}
	counts := map[string]map[string]int{
		"Plot":     {},
		"Area":     {},
		"Language": {},
		"Year":     {},
	}
	if len(allow) == 0 {
		return counts, nil
	}
	countItem := func(item *FilmSearchMeta) {
		for _, part := range shared.SplitRawSearchTags(item.ClassTag) {
			addSourceTypeTagCount(counts["Plot"], shared.NormalizeSearchTagValue("Plot", part))
		}
		addSourceTypeTagCount(counts["Area"], shared.NormalizeSearchTagValue("Area", item.Area))
		addSourceTypeTagCount(counts["Language"], shared.NormalizeSearchTagValue("Language", item.Language))
		if item.Item.Year > 0 {
			addSourceTypeTagCount(counts["Year"], strconv.FormatInt(item.Item.Year, 10))
		}
	}
	if len(idx.ByCategory) > 0 {
		for key := range allow {
			for _, pos := range idx.ByCategory[key] {
				if pos < 0 || pos >= len(idx.Items) {
					continue
				}
				countItem(&idx.Items[pos])
			}
		}
		return counts, nil
	}
	for i := range idx.Items {
		item := &idx.Items[i]
		if _, ok := allow[item.CategoryKey]; !ok {
			continue
		}
		countItem(item)
	}
	return counts, nil
}
