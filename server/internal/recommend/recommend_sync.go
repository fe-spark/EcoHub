package recommend

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"server/internal/banner"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/hotchart"
	"server/internal/model"
)

var (
	autoSyncMu      sync.Mutex
	autoSyncRunning bool
	autoSyncPending bool
)

// RecommendSyncResult 单个推荐分组同步外部榜单后的命中统计。
type RecommendSyncResult struct {
	GroupID        string `json:"groupId"`
	Title          string `json:"title"`
	ChartKey       string `json:"chartKey"`
	CandidateCount int    `json:"candidateCount"`
	MatchedCount   int    `json:"matchedCount"`
	Error          string `json:"error,omitempty"`
}

// RecommendBatchSyncResult 批量同步外部榜单的结果汇总。
type RecommendBatchSyncResult struct {
	SyncedGroups int                   `json:"syncedGroups"`
	TotalMatched int                   `json:"totalMatched"`
	FailedGroups int                   `json:"failedGroups"`
	Results      []RecommendSyncResult `json:"results"`
}

// SyncGroupNow 立即根据分组绑定的豆瓣榜单同步一次本地片库数据。
func (s *HomeRecommendService) SyncGroupNow(ctx context.Context, groupID string) (RecommendSyncResult, error) {
	group, err := GetHomeRecommendGroup(ctx, groupID)
	if err != nil {
		return RecommendSyncResult{}, err
	}
	return s.syncSingleGroup(ctx, group)
}

func (s *HomeRecommendService) syncSingleGroup(ctx context.Context, group model.HomeRecommendGroup) (RecommendSyncResult, error) {
	if group.CategoryId > 0 {
		return RecommendSyncResult{GroupID: group.Id, Title: group.Title}, errors.New("站点分类分组实时同步片库分类，无需手工同步榜单")
	}
	chartKey := strings.TrimSpace(group.SyncChartKey)
	if !isValidChartKey(chartKey) {
		return RecommendSyncResult{GroupID: group.Id, Title: group.Title}, errors.New("该分组未绑定有效的豆瓣/Bangumi榜单")
	}
	limit := int(clampSyncLimit(group.SyncLimit))

	cands, rates, err := hotchart.HotChartsSvc.GetRawCandidatesByKey(chartKey, false)
	if err != nil {
		return RecommendSyncResult{GroupID: group.Id, Title: group.Title, ChartKey: chartKey}, err
	}
	_ = filmsnapshot.WarmSearchMetaIndex()
	version := filmsnapshot.GetActiveReadModelVersion()
	mids, midToCandIdx, err := filmsnapshot.MatchHotChartCandidates(version, group.SourceId, cands, limit)
	if err != nil {
		return RecommendSyncResult{GroupID: group.Id, Title: group.Title, ChartKey: chartKey, CandidateCount: len(cands)}, err
	}

	items := make([]model.HomeRecommendItem, 0, len(mids))
	for _, mid := range mids {
		rate := ""
		if cIdx, ok := midToCandIdx[mid]; ok {
			rate = rates[cIdx]
		}
		items = append(items, model.HomeRecommendItem{
			Mid:         mid,
			DoubanScore: rate,
		})
	}
	now := time.Now()
	if err := ReplaceHomeRecommendGroupItems(ctx, group.Id, group.SourceId, items, &now); err != nil {
		return RecommendSyncResult{GroupID: group.Id, Title: group.Title, ChartKey: chartKey, CandidateCount: len(cands)}, err
	}
	invalidateHomeCacheForSource(group.SourceId)
	return RecommendSyncResult{
		GroupID:        group.Id,
		Title:          group.Title,
		ChartKey:       chartKey,
		CandidateCount: len(cands),
		MatchedCount:   len(items),
	}, nil
}

func (s *HomeRecommendService) syncGroupList(ctx context.Context, groups []model.HomeRecommendGroup) RecommendBatchSyncResult {
	summary := RecommendBatchSyncResult{
		Results: make([]RecommendSyncResult, 0, len(groups)),
	}
	if len(groups) > 0 {
		_ = filmsnapshot.WarmSearchMetaIndex()
	}
	for _, g := range groups {
		if !isValidChartKey(strings.TrimSpace(g.SyncChartKey)) {
			continue
		}
		summary.SyncedGroups++
		groupCtx := ctx
		var cancel context.CancelFunc
		if groupCtx == nil {
			groupCtx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
		}
		res, err := s.syncSingleGroup(groupCtx, g)
		if cancel != nil {
			cancel()
		}
		if err != nil {
			summary.FailedGroups++
			res.Error = err.Error()
			log.Printf("[HomeRecommend] 同步分组失败 group=%s (%s): %v", g.Id, g.Title, err)
		} else {
			summary.TotalMatched += res.MatchedCount
		}
		summary.Results = append(summary.Results, res)
	}
	return summary
}

// SyncSourceChartGroups 同步指定采集站下所有绑定了豆瓣/Bangumi榜单的分组。
func (s *HomeRecommendService) SyncSourceChartGroups(ctx context.Context, sourceID string) (RecommendBatchSyncResult, error) {
	src, _, _, err := banner.ResolveBannerSource(sourceID, true)
	if err != nil {
		return RecommendBatchSyncResult{}, err
	}
	groups, err := ListHomeRecommendGroups(ctx, src.Id)
	if err != nil {
		return RecommendBatchSyncResult{}, err
	}
	return s.syncGroupList(ctx, groups), nil
}

// SyncAllAutoGroups 在采集及检索封口完成后同步所有开启自动同步的榜单分组（串行防重入，连续触发自动合并）。
func (s *HomeRecommendService) SyncAllAutoGroups() {
	autoSyncMu.Lock()
	if autoSyncRunning {
		autoSyncPending = true
		autoSyncMu.Unlock()
		return
	}
	autoSyncRunning = true
	autoSyncMu.Unlock()

	for {
		listCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		groups, err := ListAutoSyncRecommendGroups(listCtx)
		cancel()
		if err != nil {
			log.Printf("[HomeRecommend] 查询自动同步分组失败: %v", err)
		} else if len(groups) > 0 {
			_ = s.syncGroupList(nil, groups)
		}

		autoSyncMu.Lock()
		if !autoSyncPending {
			autoSyncRunning = false
			autoSyncMu.Unlock()
			return
		}
		autoSyncPending = false
		autoSyncMu.Unlock()
	}
}
