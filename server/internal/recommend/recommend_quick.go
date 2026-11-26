package recommend

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"server/internal/banner"
	"server/internal/film/catalog"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/hotchart"
	"server/internal/model"
)

// RecommendGenerateQuota 一键排片单个推荐分组的配额。
type RecommendGenerateQuota struct {
	GroupId string `json:"groupId"`
	Count   int    `json:"count"`
}

type RecommendGenerateReq struct {
	SourceId string                   `json:"sourceId"`
	Strategy string                   `json:"strategy"` // douban | latest | hot | score | random
	Quotas   []RecommendGenerateQuota `json:"quotas"`
}

type RecommendGenerateGroupPreview struct {
	GroupId string                         `json:"groupId"`
	Title   string                         `json:"title"`
	Items   []model.HomeRecommendAdminItem `json:"items"`
}

type RecommendGenerateResult struct {
	SourceId string                          `json:"sourceId"`
	Strategy string                          `json:"strategy"`
	Groups   []RecommendGenerateGroupPreview `json:"groups"`
}

// GenerateQuickPreview 一键排片推荐分组预览（支持 douban/latest/hot/score/random 策略）。
func (s *HomeRecommendService) GenerateQuickPreview(ctx context.Context, req RecommendGenerateReq) (RecommendGenerateResult, error) {
	src, _, _, err := banner.ResolveBannerSource(req.SourceId, true)
	if err != nil {
		return RecommendGenerateResult{}, err
	}
	strategy := strings.TrimSpace(req.Strategy)
	snapStrategy, ok := banner.SnapshotStrategyKey(strategy)
	if !ok {
		return RecommendGenerateResult{}, errors.New("排片策略只能是 douban、latest、hot、score 或 random")
	}

	allGroups, err := ListHomeRecommendGroups(ctx, src.Id)
	if err != nil {
		return RecommendGenerateResult{}, err
	}
	groupByID := make(map[string]model.HomeRecommendGroup, len(allGroups))
	for _, g := range allGroups {
		groupByID[g.Id] = g
	}

	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, recommendPreviewTimeout)
	defer cancel()

	if snapStrategy == "douban" {
		_ = filmsnapshot.WarmSearchMetaIndex()
	}
	version := filmsnapshot.GetActiveSnapshotVersion()
	readVersion := filmsnapshot.GetActiveReadModelVersion()
	tree, _ := catalog.GetActiveCategoryTree(src.Id)

	out := RecommendGenerateResult{
		SourceId: src.Id,
		Strategy: strategy,
		Groups:   make([]RecommendGenerateGroupPreview, 0, len(req.Quotas)),
	}

	for _, q := range req.Quotas {
		g, exists := groupByID[strings.TrimSpace(q.GroupId)]
		if !exists || strings.TrimSpace(g.SyncChartKey) != "" || g.CategoryId > 0 || q.Count <= 0 {
			continue
		}
		count := min(q.Count, recommendSyncLimitMax)

		var previewItems []model.HomeRecommendAdminItem
		if snapStrategy == "douban" {
			previewItems, err = generateDoubanGroupPreview(ctx, readVersion, src.Id, g, count)
		} else {
			catIDs := resolveRecommendCategoryIDs(tree, g.Title, g.SyncChartKey)
			snaps, sErr := filmsnapshot.GetSnapshotBannerCandidates(ctx, version, snapStrategy, src.Id, catIDs, count, nil)
			if sErr != nil {
				return RecommendGenerateResult{}, sErr
			}
			previewItems = snapshotsToAdminItems(g.Id, src.Id, snaps, nil)
		}
		if err != nil {
			return RecommendGenerateResult{}, err
		}

		out.Groups = append(out.Groups, RecommendGenerateGroupPreview{
			GroupId: g.Id,
			Title:   g.Title,
			Items:   previewItems,
		})
	}
	return out, nil
}

func generateDoubanGroupPreview(ctx context.Context, readVersion, sourceID string, g model.HomeRecommendGroup, count int) ([]model.HomeRecommendAdminItem, error) {
	var cands []filmsnapshot.HotChartCandidate
	var rates map[int]string
	if isValidChartKey(g.SyncChartKey) {
		cands, rates, _ = hotchart.HotChartsSvc.GetRawCandidatesByKey(g.SyncChartKey, false)
	} else {
		cands, _ = hotchart.HotChartsSvc.GetRawCandidatesByCategoryName(g.Title)
	}
	mids, midToIdx, err := filmsnapshot.MatchHotChartCandidates(readVersion, sourceID, cands, count)
	if err != nil {
		return nil, err
	}
	snaps, err := filmsnapshot.ListSnapshotsByMIDsForHotCharts(ctx, mids)
	if err != nil {
		return nil, err
	}
	rateByMid := make(map[int64]string, len(mids))
	if rates != nil {
		for _, mid := range mids {
			if idx, ok := midToIdx[mid]; ok {
				rateByMid[mid] = rates[idx]
			}
		}
	}
	return snapshotsToAdminItems(g.Id, sourceID, snaps, rateByMid), nil
}

func snapshotsToAdminItems(groupID, sourceID string, snaps []model.FilmListSnapshot, rateByMid map[int64]string) []model.HomeRecommendAdminItem {
	items := make([]model.HomeRecommendAdminItem, 0, len(snaps))
	for i, sn := range snaps {
		yr := ""
		if sn.Year > 0 {
			yr = fmt.Sprintf("%d", sn.Year)
		}
		items = append(items, model.HomeRecommendAdminItem{
			GroupId:     groupID,
			SourceId:    sourceID,
			Mid:         sn.Mid,
			Sort:        int64(i + 1),
			DoubanScore: rateByMid[sn.Mid],
			Name:        sn.Name,
			Picture:     sn.DisplayPicture(),
			Year:        yr,
			CName:       sn.CName,
			Area:        sn.Area,
			Remarks:     sn.Remarks,
			Score:       sn.Score,
		})
	}
	return items
}

func resolveRecommendCategoryIDs(tree model.CategoryTree, title, chartKey string) []int64 {
	kw := ""
	switch {
	case chartKey == "movie_showing" || chartKey == "movie_hot" || strings.Contains(title, "电影") || strings.Contains(title, "热映"):
		kw = "电影"
	case chartKey == "variety_hot" || strings.Contains(title, "综艺"):
		kw = "综艺"
	case chartKey == "bangumi_calendar" || strings.Contains(title, "动漫") || strings.Contains(title, "动画") || strings.Contains(title, "番"):
		kw = "动漫"
	case chartKey == "tv_hot" || strings.Contains(title, "剧"):
		kw = "剧"
	}
	ids := make([]int64, 0, len(tree.Children))
	for _, ch := range tree.Children {
		if ch == nil || ch.Id <= 0 {
			continue
		}
		if kw == "" || strings.Contains(ch.Name, kw) {
			ids = append(ids, ch.Id)
		}
	}
	if len(ids) == 0 {
		for _, ch := range tree.Children {
			if ch != nil && ch.Id > 0 {
				ids = append(ids, ch.Id)
			}
		}
	}
	return ids
}

type RecommendDeckGroupPayload struct {
	GroupId string `json:"groupId"`
	Items   []struct {
		Mid         int64  `json:"mid"`
		DoubanScore string `json:"doubanScore"`
	} `json:"items"`
}

type RecommendDeckSaveReq struct {
	SourceId string                      `json:"sourceId"`
	Groups   []RecommendDeckGroupPayload `json:"groups"`
}

// PublishQuickDeck 发布一键排片预览到各个推荐分组。
func (s *HomeRecommendService) PublishQuickDeck(ctx context.Context, req RecommendDeckSaveReq) error {
	src, _, _, err := banner.ResolveBannerSource(req.SourceId, true)
	if err != nil {
		return err
	}
	allMids := make([]int64, 0)
	for _, gp := range req.Groups {
		for _, it := range gp.Items {
			if it.Mid > 0 {
				allMids = append(allMids, it.Mid)
			}
		}
	}
	onSource, err := filmsnapshot.VisiblePlayableMIDs(ctx, src.Id, allMids)
	if err != nil {
		return err
	}
	for _, gp := range req.Groups {
		g, err := GetHomeRecommendGroup(ctx, gp.GroupId)
		if err != nil || g.SourceId != src.Id || strings.TrimSpace(g.SyncChartKey) != "" || g.CategoryId > 0 {
			continue
		}
		items := make([]model.HomeRecommendItem, 0, len(gp.Items))
		seen := make(map[int64]bool, len(gp.Items))
		for _, it := range gp.Items {
			if it.Mid <= 0 || seen[it.Mid] || !onSource[it.Mid] {
				continue
			}
			seen[it.Mid] = true
			items = append(items, model.HomeRecommendItem{
				Mid:         it.Mid,
				DoubanScore: strings.TrimSpace(it.DoubanScore),
			})
		}
		if err := ReplaceHomeRecommendGroupItems(ctx, g.Id, src.Id, items, nil); err != nil {
			return err
		}
	}
	invalidateHomeCacheForSource(src.Id)
	return nil
}
