package notify

import (
	"context"
	"server/internal/film"
	"server/internal/film/catalog"
	"server/internal/film/snapshot"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repobase"
	"server/internal/sourcecatalog"
	"server/internal/taxonomy"
	"server/internal/utils"
	"strings"
	"time"
)

const notifySourceStatsTimeout = 10 * time.Second

func buildNotifyBatchSession(payload model.CollectBatchNotifyPayload, pageSize int) (model.NotifyBatchSession, error) {
	if pageSize <= 0 {
		pageSize = model.DefaultNotifyPageSize
	} else if pageSize > model.MaxNotifyPageSize {
		pageSize = model.MaxNotifyPageSize
	}
	basic, err := repobase.GetSiteBasic()
	if err != nil {
		return model.NotifyBatchSession{}, err
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
	hasLedger := false
	primaryStatsUnavailable := false
	statsBySource := make(map[string][]BatchSourceCategoryStatRow, len(payload.Sources))
	statsFailed := make(map[string]bool, len(payload.Sources))
	if collectBatchID != "" {
		ok, err := HasCollectBatchLedger(collectBatchID)
		if err != nil {
			syslog.Warnf("[Notify] 查询账本状态失败，降级仅发汇总 batch=%s err=%v", collectBatchID, err)
			if activeSourceID != "" {
				primaryStatsUnavailable = true
			}
		} else if ok {
			hasLedger = true
			seenSources := make(map[string]struct{}, len(payload.Sources)+1)
			loadStats := func(sid string) {
				sid = strings.TrimSpace(sid)
				if sid == "" {
					return
				}
				if _, done := seenSources[sid]; done {
					return
				}
				seenSources[sid] = struct{}{}
				statsCtx, cancel := context.WithTimeout(context.Background(), notifySourceStatsTimeout)
				rows, statErr := loadBatchSourceCategoryStatsFn(statsCtx, collectBatchID, sid)
				cancel()
				if statErr != nil {
					syslog.Warnf("[Notify] 统计站点分类失败，降级仅发汇总 batch=%s source=%s err=%v", collectBatchID, sid, statErr)
					statsFailed[sid] = true
					if sid == activeSourceID {
						primaryStatsUnavailable = true
					}
					return
				}
				statsBySource[sid] = rows
			}
			for _, src := range payload.Sources {
				loadStats(src.SourceID)
			}
			if activeSourceID != "" {
				loadStats(activeSourceID)
			}
		}
	}

	visibleCount := 0
	if primaryStatsUnavailable {
		visibleCount = -1
	} else {
		visibleCount = countPrimaryVisibleFromStats(activeSourceID, statsBySource[activeSourceID])
	}

	sources := make([]model.NotifySourceSession, 0, len(payload.Sources))
	for _, src := range payload.Sources {
		srcID := strings.TrimSpace(src.SourceID)
		isPrimary := activeSourceID != "" && srcID == activeSourceID
		var (
			srcCats     []model.NotifySourceCategoryGroup
			srcAllCount int
		)
		if hasLedger {
			if statsFailed[srcID] {
				srcAllCount = src.FilmsTotal
			} else {
				var catErr error
				srcCats, srcAllCount, catErr = buildSourceCategoryCountsFromStats(srcID, statsBySource[srcID])
				if catErr != nil {
					syslog.Warnf("[Notify] 聚合站点分类导航失败，降级仅发汇总 batch=%s source=%s err=%v", collectBatchID, srcID, catErr)
					srcCats = nil
					srcAllCount = src.FilmsTotal
				}
			}
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
		})
	}

	batchID, err := utils.GenerateSalt()
	if err != nil {
		return model.NotifyBatchSession{}, err
	}
	if len(batchID) > 10 {
		batchID = batchID[:10]
	}
	overview := formatBatchOverviewHTML(siteName, payload, activeSourceName, visibleCount)

	sessionCollectBatchID := ""
	if hasLedger {
		sessionCollectBatchID = collectBatchID
	}
	return model.NotifyBatchSession{
		BatchID:        batchID,
		CollectBatchID: sessionCollectBatchID,
		SiteName:       siteName,
		OverviewHTML:   overview,
		PageSize:       pageSize,
		Sources:        sources,
	}, nil
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

func (m sourceCategoryMatcher) buildPageQuery(batchID, sourceID string, cats []model.NotifySourceCategoryGroup, catIdx, limit, offset int) BatchSourceCategoryPageQuery {
	q := BatchSourceCategoryPageQuery{
		BatchID:         batchID,
		SourceID:        sourceID,
		UseCategoryKeys: m.useSourceKeys,
		KnownKeys:       m.allKeys,
		KnownPids:       m.allPids,
		Limit:           limit,
		Offset:          offset,
	}
	if catIdx < 0 || catIdx >= len(cats) {
		q.CategoryAll = true
		return q
	}
	cat := cats[catIdx]
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
