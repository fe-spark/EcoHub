package notify

import (
	"server/internal/film"
	"server/internal/film/catalog"
	"server/internal/film/snapshot"
	"server/internal/model"
	"server/internal/repobase"
	"server/internal/sourcecatalog"
	"server/internal/taxonomy"
	"server/internal/utils"
	"sort"
	"strings"
)

func buildNotifyBatchSession(payload model.CollectBatchNotifyPayload, pageSize int) (model.NotifyBatchSession, error) {
	sess, _, err := buildNotifyBatchSessionWithCount(payload, pageSize)
	return sess, err
}

func buildNotifyBatchSessionWithCount(payload model.CollectBatchNotifyPayload, pageSize int) (model.NotifyBatchSession, int, error) {
	if pageSize <= 0 {
		pageSize = model.DefaultNotifyPageSize
	} else if pageSize > model.MaxNotifyPageSize {
		pageSize = model.MaxNotifyPageSize
	}
	basic, err := repobase.GetSiteBasic()
	if err != nil {
		return model.NotifyBatchSession{}, 0, err
	}
	siteName := basic.SiteName

	var activeSourceID, activeSourceName string
	if active := sourcecatalog.CurrentPrimarySource(); active != nil {
		activeSourceID = strings.TrimSpace(active.Id)
		activeSourceName = strings.TrimSpace(active.Name)
		if activeSourceName == "" {
			activeSourceName = activeSourceID
		}
	}

	collectBatchID := strings.TrimSpace(payload.CollectBatchID)
	useLedger := false
	var statsBySource map[string][]BatchSourceCategoryStatRow
	if collectBatchID != "" {
		hasLedger, err := HasCollectBatchLedger(collectBatchID)
		if err != nil {
			return model.NotifyBatchSession{}, 0, err
		}
		if hasLedger {
			statsRows, err := LoadBatchSourceCategoryStats(collectBatchID)
			if err != nil {
				return model.NotifyBatchSession{}, 0, err
			}
			useLedger = true
			statsBySource = make(map[string][]BatchSourceCategoryStatRow)
			for _, r := range statsRows {
				sid := strings.TrimSpace(r.SourceID)
				statsBySource[sid] = append(statsBySource[sid], r)
			}
		}
	}

	var (
		metaByMid map[int64]NotifyFilmMeta
		orderRank map[int64]int
	)
	if !(useLedger && len(payload.Sources) > 0) {
		uniqueMIDs := collectBatchUniqueMIDs(payload)
		metaByMid = make(map[int64]NotifyFilmMeta, len(uniqueMIDs))
		orderRank = make(map[int64]int, len(uniqueMIDs))
		if len(uniqueMIDs) > 0 {
			metas, err := LoadNotifyFilmMetas(uniqueMIDs)
			if err != nil {
				return model.NotifyBatchSession{}, 0, err
			}
			for idx, m := range metas {
				metaByMid[m.Mid] = m
				orderRank[m.Mid] = idx
			}
		}
	}

	// 有 Sources 时每个站点已各自保存轻量化计数或 ID 列表，不再把主站全量片名重复写入 AllFilms/Categories
	var (
		allFilms     []model.NotifyFilmItem
		categories   []model.NotifyCategoryGroup
		visibleCount int
	)
	if len(payload.Sources) > 0 {
		if useLedger {
			visibleCount = countPrimaryVisibleFromStats(activeSourceID, statsBySource[activeSourceID])
		} else {
			visibleCount = len(filterPrimaryVisibleMetas(activeSourceID, payload.NotifyMIDs, metaByMid, orderRank))
		}
	} else {
		allFilms, categories, err = buildPrimaryVisibleGroups(activeSourceID, payload.NotifyMIDs, metaByMid, orderRank)
		if err != nil {
			return model.NotifyBatchSession{}, 0, err
		}
		visibleCount = len(allFilms)
	}

	sources := make([]model.NotifySourceSession, 0, len(payload.Sources))
	for _, src := range payload.Sources {
		srcID := strings.TrimSpace(src.SourceID)
		isPrimary := activeSourceID != "" && srcID == activeSourceID
		var (
			srcCats     []model.NotifySourceCategoryGroup
			srcAllMids  []int64
			srcAllCount int
		)
		if useLedger {
			srcCats, srcAllCount, err = buildSourceCategoryCountsFromStats(srcID, statsBySource[srcID])
		} else {
			srcMetas := resolveOrderedMetas(src.FilmMIDs, metaByMid, orderRank)
			srcCats, srcAllMids, err = buildSourceCategoryMids(srcID, srcMetas)
			srcAllCount = len(srcAllMids)
		}
		if err != nil {
			return model.NotifyBatchSession{}, 0, err
		}
		displayTotal := src.FilmsTotal
		if srcAllCount > displayTotal {
			displayTotal = srcAllCount
		}
		sources = append(sources, model.NotifySourceSession{
			SourceID:     srcID,
			SourceName:   strings.TrimSpace(src.SourceName),
			IsPrimary:    isPrimary,
			Status:       src.Status,
			Error:        src.Error,
			PageTotal:    src.PageTotal,
			SuccessCnt:   src.SuccessCnt,
			FailedCnt:    src.FailedCnt,
			FilmsTotal:   displayTotal,
			AllCount:     srcAllCount,
			CategorySkip: src.CategorySkip,
			OverviewHTML: formatSourceDetailHTML(siteName, src, isPrimary, srcAllCount),
			Categories:   srcCats,
			AllMids:      srcAllMids,
		})
	}

	batchID, err := utils.GenerateSalt()
	if err != nil {
		return model.NotifyBatchSession{}, 0, err
	}
	if len(batchID) > 10 {
		batchID = batchID[:10]
	}
	overview := formatBatchOverviewHTML(siteName, payload, activeSourceName, visibleCount)

	sessionCollectBatchID := ""
	if useLedger {
		sessionCollectBatchID = collectBatchID
	}
	return model.NotifyBatchSession{
		BatchID:        batchID,
		CollectBatchID: sessionCollectBatchID,
		SiteName:       siteName,
		OverviewHTML:   overview,
		PageSize:       pageSize,
		Categories:     categories,
		AllFilms:       allFilms,
		Sources:        sources,
	}, visibleCount, nil
}

func collectBatchUniqueMIDs(payload model.CollectBatchNotifyPayload) []int64 {
	seen := make(map[int64]struct{}, len(payload.NotifyMIDs))
	out := make([]int64, 0, len(payload.NotifyMIDs))
	add := func(mids []int64) {
		for _, mid := range mids {
			if mid <= 0 {
				continue
			}
			if _, ok := seen[mid]; ok {
				continue
			}
			seen[mid] = struct{}{}
			out = append(out, mid)
		}
	}
	add(payload.NotifyMIDs)
	for _, src := range payload.Sources {
		add(src.FilmMIDs)
	}
	return out
}

func resolveOrderedMetas(mids []int64, metaByMid map[int64]NotifyFilmMeta, orderRank map[int64]int) []NotifyFilmMeta {
	if len(mids) == 0 || len(metaByMid) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(mids))
	out := make([]NotifyFilmMeta, 0, len(mids))
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		m, exists := metaByMid[mid]
		if !exists {
			continue
		}
		seen[mid] = struct{}{}
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return orderRank[out[i].Mid] < orderRank[out[j].Mid]
	})
	return out
}

func filterPrimaryVisibleMetas(activeSourceID string, notifyMIDs []int64, metaByMid map[int64]NotifyFilmMeta, orderRank map[int64]int) []NotifyFilmMeta {
	if activeSourceID == "" || len(notifyMIDs) == 0 {
		return nil
	}
	metas := resolveOrderedMetas(notifyMIDs, metaByMid, orderRank)
	if len(metas) == 0 {
		return nil
	}
	visible := snapshot.SourceCategoryVisibility(activeSourceID)
	filtered := make([]NotifyFilmMeta, 0, len(metas))
	for _, m := range metas {
		if strings.TrimSpace(m.FirstSourceId) != activeSourceID {
			continue
		}
		if !visible(m.CategoryKey, m.RootCategoryKey, m.Cid, m.Pid) {
			continue
		}
		filtered = append(filtered, m)
	}
	return filtered
}

func buildPrimaryVisibleGroups(activeSourceID string, notifyMIDs []int64, metaByMid map[int64]NotifyFilmMeta, orderRank map[int64]int) ([]model.NotifyFilmItem, []model.NotifyCategoryGroup, error) {
	filtered := filterPrimaryVisibleMetas(activeSourceID, notifyMIDs, metaByMid, orderRank)
	if len(filtered) == 0 {
		return nil, nil, nil
	}
	srcCats, allMids, err := buildSourceCategoryMids(activeSourceID, filtered)
	if err != nil {
		return nil, nil, err
	}
	allFilms := make([]model.NotifyFilmItem, 0, len(allMids))
	for _, mid := range allMids {
		m := metaByMid[mid]
		allFilms = append(allFilms, model.NotifyFilmItem{Mid: m.Mid, Name: m.Name})
	}
	categories := make([]model.NotifyCategoryGroup, 0, len(srcCats))
	for _, sc := range srcCats {
		items := make([]model.NotifyFilmItem, 0, len(sc.Mids))
		for _, mid := range sc.Mids {
			m := metaByMid[mid]
			items = append(items, model.NotifyFilmItem{Mid: m.Mid, Name: m.Name})
		}
		categories = append(categories, model.NotifyCategoryGroup{
			CategoryID:   sc.CategoryID,
			CategoryName: sc.CategoryName,
			Films:        items,
		})
	}
	return allFilms, categories, nil
}

type sourceCategoryMatcher struct {
	nav           []model.Category
	navSet        map[int64]struct{}
	keyToRoot     map[string]int64
	keysByRoot    map[int64][]string
	allKeys       []string
	allPids       []int64
	useSourceKeys bool
}

func loadSourceCategoryMatcher(sourceID string) (sourceCategoryMatcher, error) {
	nav, err := film.NavTopCategories(sourceID)
	if err != nil {
		return sourceCategoryMatcher{}, err
	}
	m := sourceCategoryMatcher{
		nav:        nav,
		navSet:     make(map[int64]struct{}, len(nav)),
		keyToRoot:  make(map[string]int64, len(nav)*4),
		keysByRoot: make(map[int64][]string, len(nav)),
	}
	for _, c := range nav {
		if c.Id <= 0 {
			continue
		}
		m.navSet[c.Id] = struct{}{}
		m.allPids = append(m.allPids, c.Id)
		if sourceID != "" {
			for _, typeID := range catalog.PublicSourceTypeIDs(sourceID, "pid", c.Id) {
				if k := taxonomy.BuildSourceCategoryKey(sourceID, typeID); k != "" {
					if _, exists := m.keyToRoot[k]; !exists {
						m.keyToRoot[k] = c.Id
						m.keysByRoot[c.Id] = append(m.keysByRoot[c.Id], k)
						m.allKeys = append(m.allKeys, k)
					}
				}
			}
		}
	}
	m.useSourceKeys = len(m.keyToRoot) > 0
	return m, nil
}

func (m sourceCategoryMatcher) matchRootID(categoryKey, rootCategoryKey string, pid int64) (int64, bool) {
	if m.useSourceKeys {
		if rootID, ok := m.keyToRoot[categoryKey]; ok && rootID > 0 {
			return rootID, true
		}
		if rootID, ok := m.keyToRoot[rootCategoryKey]; ok && rootID > 0 {
			return rootID, true
		}
		return 0, false
	}
	if pid > 0 {
		if _, ok := m.navSet[pid]; ok {
			return pid, true
		}
	}
	return 0, false
}

func (m sourceCategoryMatcher) buildPageQuery(batchID, sourceID string, catIdx int, cat model.NotifySourceCategoryGroup, limit, offset int) BatchSourceCategoryPageQuery {
	q := BatchSourceCategoryPageQuery{
		BatchID:         batchID,
		SourceID:        sourceID,
		UseCategoryKeys: m.useSourceKeys,
		KnownKeys:       m.allKeys,
		KnownPids:       m.allPids,
		Limit:           limit,
		Offset:          offset,
	}
	if catIdx < 0 {
		q.CategoryAll = true
		return q
	}
	if cat.CategoryID == 0 {
		q.IsOther = true
		return q
	}
	q.IncludeKeys = m.keysByRoot[cat.CategoryID]
	q.IncludePids = []int64{cat.CategoryID}
	return q
}

func countPrimaryVisibleFromStats(activeSourceID string, rows []BatchSourceCategoryStatRow) int {
	if activeSourceID == "" || len(rows) == 0 {
		return 0
	}
	visible := snapshot.SourceCategoryVisibility(activeSourceID)
	total := 0
	for _, r := range rows {
		if r.Count <= 0 || strings.TrimSpace(r.FirstSourceId) != activeSourceID {
			continue
		}
		if visible(r.CategoryKey, r.RootCategoryKey, r.Cid, r.Pid) {
			total += r.Count
		}
	}
	return total
}

func buildSourceCategoryCountsFromStats(sourceID string, rows []BatchSourceCategoryStatRow) ([]model.NotifySourceCategoryGroup, int, error) {
	if len(rows) == 0 {
		return nil, 0, nil
	}
	matcher, err := loadSourceCategoryMatcher(sourceID)
	if err != nil {
		return nil, 0, err
	}
	allCount := 0
	buckets := make(map[int64]int, len(matcher.nav))
	otherCount := 0
	for _, r := range rows {
		if r.Count <= 0 {
			continue
		}
		allCount += r.Count
		if rootID, ok := matcher.matchRootID(r.CategoryKey, r.RootCategoryKey, r.Pid); ok {
			buckets[rootID] += r.Count
		} else {
			otherCount += r.Count
		}
	}
	if allCount == 0 {
		return nil, 0, nil
	}
	groups := make([]model.NotifySourceCategoryGroup, 0, len(matcher.nav)+1)
	for _, c := range matcher.nav {
		if cnt := buckets[c.Id]; cnt > 0 {
			groups = append(groups, model.NotifySourceCategoryGroup{
				CategoryID:   c.Id,
				CategoryName: c.Name,
				Count:        cnt,
			})
		}
	}
	if otherCount > 0 {
		groups = append(groups, model.NotifySourceCategoryGroup{
			CategoryID:   0,
			CategoryName: "其他",
			Count:        otherCount,
		})
	}
	return groups, allCount, nil
}

func buildSourceCategoryMids(sourceID string, metas []NotifyFilmMeta) ([]model.NotifySourceCategoryGroup, []int64, error) {
	if len(metas) == 0 {
		return nil, nil, nil
	}
	matcher, err := loadSourceCategoryMatcher(sourceID)
	if err != nil {
		return nil, nil, err
	}

	allMids := make([]int64, 0, len(metas))
	buckets := make(map[int64][]int64, len(matcher.nav))
	var others []int64
	for _, m := range metas {
		allMids = append(allMids, m.Mid)
		if rootID, ok := matcher.matchRootID(m.CategoryKey, m.RootCategoryKey, m.Pid); ok {
			buckets[rootID] = append(buckets[rootID], m.Mid)
		} else {
			others = append(others, m.Mid)
		}
	}

	groups := make([]model.NotifySourceCategoryGroup, 0, len(matcher.nav)+1)
	for _, c := range matcher.nav {
		if list := buckets[c.Id]; len(list) > 0 {
			groups = append(groups, model.NotifySourceCategoryGroup{
				CategoryID:   c.Id,
				CategoryName: c.Name,
				Count:        len(list),
				Mids:         list,
			})
		}
	}
	if len(others) > 0 {
		groups = append(groups, model.NotifySourceCategoryGroup{
			CategoryID:   0,
			CategoryName: "其他",
			Count:        len(others),
			Mids:         others,
		})
	}
	return groups, allMids, nil
}
