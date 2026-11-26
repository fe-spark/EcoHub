package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/repository/support"
)

type HomeRecommendService struct{}

var HomeRecommendSvc = new(HomeRecommendService)

const (
	recommendSyncLimitDefault = 12
	recommendSyncLimitMax     = 24
	recommendPreviewTimeout   = 15 * time.Second
)

// ChartOption 可选绑定的豆瓣/Bangumi 榜单描述。
type ChartOption struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

var RecommendChartOptions = []ChartOption{
	{Key: "movie_showing", Title: "院线热映"},
	{Key: "movie_hot", Title: "热门电影"},
	{Key: "tv_hot", Title: "热门剧集"},
	{Key: "variety_hot", Title: "热门综艺"},
	{Key: "bangumi_calendar", Title: "当季新番"},
}

func isValidChartKey(key string) bool {
	for _, opt := range RecommendChartOptions {
		if opt.Key == key {
			return true
		}
	}
	return false
}

func chartTitleByKey(key string) string {
	for _, opt := range RecommendChartOptions {
		if opt.Key == key {
			return opt.Title
		}
	}
	return key
}

func invalidateHomeCacheForSource(sourceID string) {
	support.BumpIndexPageGeneration()
	if db.Rdb == nil {
		return
	}
	version := filmsnapshot.GetActiveReadModelVersion()
	cacheKey := fmt.Sprintf("%s:v%s:src_%s:home", config.IndexPageCacheKey, version, strings.TrimSpace(sourceID))
	_ = db.Rdb.Del(db.Cxt, cacheKey).Err()
}

// HomeRecommendListResult 后台列表返回。
type HomeRecommendListResult struct {
	SourceId        string                         `json:"sourceId"`
	PrimarySourceId string                         `json:"primarySourceId"`
	ChartOptions    []ChartOption                  `json:"chartOptions"`
	Groups          []model.HomeRecommendAdminGroup `json:"groups"`
}

// EnsureBuiltinGroups 确保指定采集站拥有内置分组（每日更新 + 顶级可见分类）。
func (s *HomeRecommendService) EnsureBuiltinGroups(ctx context.Context, sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil
	}
	existing, err := repository.ListHomeRecommendGroups(ctx, sourceID)
	if err != nil {
		return err
	}
	byBuiltinKey := make(map[string]model.HomeRecommendGroup, len(existing))
	for _, g := range existing {
		if g.Builtin && g.BuiltinKey != "" {
			byBuiltinKey[g.BuiltinKey] = g
		}
	}

	type builtinSpec struct {
		key   string
		title string
	}
	specs := []builtinSpec{{key: "daily_updates", title: "每日更新"}}
	if tree, err := repository.GetActiveCategoryTree(sourceID); err == nil {
		const maxHomeSections = 12
		children := tree.Children
		if len(children) > maxHomeSections {
			children = children[:maxHomeSections]
		}
		for _, ch := range children {
			if ch == nil || ch.Id <= 0 || !ch.Show {
				continue
			}
			specs = append(specs, builtinSpec{
				key:   fmt.Sprintf("cat:%d", ch.Id),
				title: ch.Name,
			})
		}
	}

	changed := false
	for _, sp := range specs {
		if cur, ok := byBuiltinKey[sp.key]; !ok {
			if _, err := repository.CreateHomeRecommendGroup(ctx, model.HomeRecommendGroup{
				SourceId:   sourceID,
				Title:      sp.title,
				Enabled:    true,
				Builtin:    true,
				BuiltinKey: sp.key,
			}); err != nil {
				return err
			}
			changed = true
		} else if cur.Title != sp.title && sp.title != "" {
			cur.Title = sp.title
			_ = repository.UpdateHomeRecommendGroup(ctx, cur)
			changed = true
		}
	}
	if changed {
		invalidateHomeCacheForSource(sourceID)
	}
	return nil
}

// ListAdmin 获取指定采集站的后台推荐分组及影片列表。
func (s *HomeRecommendService) ListAdmin(ctx context.Context, sourceID string) (HomeRecommendListResult, error) {
	src, _, primaryID, err := resolveBannerSource(sourceID, true)
	if err != nil {
		return HomeRecommendListResult{}, err
	}
	if err := s.EnsureBuiltinGroups(ctx, src.Id); err != nil {
		return HomeRecommendListResult{}, err
	}
	groups, err := repository.ListHomeRecommendGroups(ctx, src.Id)
	if err != nil {
		return HomeRecommendListResult{}, err
	}
	items, err := repository.ListHomeRecommendItemsBySource(ctx, src.Id)
	if err != nil {
		return HomeRecommendListResult{}, err
	}

	mids := make([]int64, 0, len(items))
	for _, it := range items {
		if it.Mid > 0 {
			mids = append(mids, it.Mid)
		}
	}
	snaps, _ := filmsnapshot.ListSnapshotsByMIDsForHotCharts(ctx, mids)
	byMid := make(map[int64]model.FilmListSnapshot, len(snaps))
	for _, sn := range snaps {
		byMid[sn.Mid] = sn
	}

	itemsByGroup := make(map[string][]model.HomeRecommendAdminItem)
	for _, it := range items {
		sn, ok := byMid[it.Mid]
		adminItem := model.HomeRecommendAdminItem{
			ItemId:      it.Id,
			GroupId:     it.GroupId,
			SourceId:    it.SourceId,
			Mid:         it.Mid,
			Sort:        it.Sort,
			DoubanScore: it.DoubanScore,
			Unavailable: !ok,
		}
		if ok {
			adminItem.Name = sn.Name
			adminItem.Picture = sn.Picture
			if sn.Year > 0 {
				adminItem.Year = fmt.Sprintf("%d", sn.Year)
			}
			adminItem.CName = sn.CName
			adminItem.Area = sn.Area
			adminItem.Remarks = sn.Remarks
			adminItem.Score = sn.Score
		} else {
			adminItem.Name = fmt.Sprintf("已下线影片 #%d", it.Mid)
		}
		itemsByGroup[it.GroupId] = append(itemsByGroup[it.GroupId], adminItem)
	}

	out := make([]model.HomeRecommendAdminGroup, 0, len(groups))
	for _, g := range groups {
		gItems := itemsByGroup[g.Id]
		if gItems == nil {
			gItems = make([]model.HomeRecommendAdminItem, 0)
		}
		out = append(out, model.HomeRecommendAdminGroup{
			HomeRecommendGroup: g,
			Items:              gItems,
		})
	}

	return HomeRecommendListResult{
		SourceId:        src.Id,
		PrimarySourceId: primaryID,
		ChartOptions:    RecommendChartOptions,
		Groups:          out,
	}, nil
}

// InitPresetGroups 为指定采集站初始化 5 个默认推荐分组（已存在同名分组则跳过）。
func (s *HomeRecommendService) InitPresetGroups(ctx context.Context, sourceID string) ([]model.HomeRecommendGroup, error) {
	src, _, _, err := resolveBannerSource(sourceID, true)
	if err != nil {
		return nil, err
	}
	existing, err := repository.ListHomeRecommendGroups(ctx, src.Id)
	if err != nil {
		return nil, err
	}
	existByChart := make(map[string]bool, len(existing))
	existByTitle := make(map[string]bool, len(existing))
	for _, g := range existing {
		if g.SyncChartKey != "" {
			existByChart[g.SyncChartKey] = true
		}
		existByTitle[g.Title] = true
	}

	for _, opt := range RecommendChartOptions {
		if existByChart[opt.Key] || existByTitle[opt.Title] {
			continue
		}
		created, err := repository.CreateHomeRecommendGroup(ctx, model.HomeRecommendGroup{
			SourceId:     src.Id,
			Title:        opt.Title,
			Enabled:      true,
			AutoSync:     true,
			SyncChartKey: opt.Key,
			SyncLimit:    recommendSyncLimitDefault,
		})
		if err != nil {
			return nil, err
		}
		// 创建后顺便尝试立即同步一次数据
		_ = s.SyncGroupNow(ctx, created.Id)
	}
	invalidateHomeCacheForSource(src.Id)
	return repository.ListHomeRecommendGroups(ctx, src.Id)
}

type RecommendGroupInput struct {
	SourceId     string `json:"sourceId"`
	Title        string `json:"title"`
	Enabled      *bool  `json:"enabled"`
	AutoSync     *bool  `json:"autoSync"`
	SyncChartKey string `json:"syncChartKey"`
	SyncLimit    int64  `json:"syncLimit"`
}

func (s *HomeRecommendService) CreateGroup(ctx context.Context, req RecommendGroupInput) (model.HomeRecommendGroup, error) {
	src, _, _, err := resolveBannerSource(req.SourceId, true)
	if err != nil {
		return model.HomeRecommendGroup{}, err
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return model.HomeRecommendGroup{}, errors.New("分组名称不能为空")
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	autoSync := false
	if req.AutoSync != nil {
		autoSync = *req.AutoSync
	}
	chartKey := strings.TrimSpace(req.SyncChartKey)
	if autoSync && !isValidChartKey(chartKey) {
		return model.HomeRecommendGroup{}, errors.New("开启自动跟随豆瓣榜单时，请选择有效的榜单类型")
	}
	limit := req.SyncLimit
	if limit <= 0 {
		limit = recommendSyncLimitDefault
	} else if limit > recommendSyncLimitMax {
		limit = recommendSyncLimitMax
	}

	created, err := repository.CreateHomeRecommendGroup(ctx, model.HomeRecommendGroup{
		SourceId:     src.Id,
		Title:        title,
		Enabled:      enabled,
		AutoSync:     autoSync,
		SyncChartKey: chartKey,
		SyncLimit:    limit,
	})
	if err != nil {
		return model.HomeRecommendGroup{}, err
	}
	if created.AutoSync {
		_ = s.SyncGroupNow(ctx, created.Id)
	}
	invalidateHomeCacheForSource(src.Id)
	return created, nil
}

func (s *HomeRecommendService) UpdateGroup(ctx context.Context, id string, req RecommendGroupInput) error {
	existing, err := repository.GetHomeRecommendGroup(ctx, id)
	if err != nil {
		return err
	}
	if existing.Builtin {
		if req.Enabled != nil {
			existing.Enabled = *req.Enabled
		}
		if err := repository.UpdateHomeRecommendGroup(ctx, existing); err != nil {
			return err
		}
		invalidateHomeCacheForSource(existing.SourceId)
		return nil
	}
	if strings.TrimSpace(req.Title) != "" {
		existing.Title = strings.TrimSpace(req.Title)
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if req.AutoSync != nil {
		existing.AutoSync = *req.AutoSync
	}
	if req.SyncChartKey != "" || !existing.AutoSync {
		existing.SyncChartKey = strings.TrimSpace(req.SyncChartKey)
	}
	if existing.AutoSync && !isValidChartKey(existing.SyncChartKey) {
		return errors.New("开启自动跟随豆瓣榜单时，请选择有效的榜单类型")
	}
	if req.SyncLimit > 0 {
		if req.SyncLimit > recommendSyncLimitMax {
			existing.SyncLimit = recommendSyncLimitMax
		} else {
			existing.SyncLimit = req.SyncLimit
		}
	}
	if err := repository.UpdateHomeRecommendGroup(ctx, existing); err != nil {
		return err
	}
	invalidateHomeCacheForSource(existing.SourceId)
	return nil
}

func (s *HomeRecommendService) DeleteGroup(ctx context.Context, id string) error {
	existing, err := repository.GetHomeRecommendGroup(ctx, id)
	if err != nil {
		return err
	}
	if existing.Builtin {
		return errors.New("内置分组不可删除，可通过开关控制显示")
	}
	if err := repository.DeleteHomeRecommendGroup(ctx, id); err != nil {
		return err
	}
	invalidateHomeCacheForSource(existing.SourceId)
	return nil
}

func (s *HomeRecommendService) ReorderGroups(ctx context.Context, sourceID string, ids []string) error {
	src, _, _, err := resolveBannerSource(sourceID, true)
	if err != nil {
		return err
	}
	if err := repository.ReorderHomeRecommendGroups(ctx, src.Id, ids); err != nil {
		return err
	}
	invalidateHomeCacheForSource(src.Id)
	return nil
}

func (s *HomeRecommendService) AddItem(ctx context.Context, groupID string, mid int64, doubanScore string) (model.HomeRecommendItem, error) {
	if mid <= 0 {
		return model.HomeRecommendItem{}, errors.New("影片编号不合法")
	}
	group, err := repository.GetHomeRecommendGroup(ctx, groupID)
	if err != nil {
		return model.HomeRecommendItem{}, err
	}
	if group.Builtin {
		return model.HomeRecommendItem{}, errors.New("内置分组由系统自动维护内容")
	}
	item, err := repository.AddHomeRecommendItem(ctx, group.Id, group.SourceId, mid, doubanScore)
	if err != nil {
		return model.HomeRecommendItem{}, err
	}
	invalidateHomeCacheForSource(group.SourceId)
	return item, nil
}

func (s *HomeRecommendService) ReorderItems(ctx context.Context, groupID string, itemIDs []string) error {
	group, err := repository.GetHomeRecommendGroup(ctx, groupID)
	if err != nil {
		return err
	}
	if err := repository.ReorderHomeRecommendItems(ctx, group.Id, itemIDs); err != nil {
		return err
	}
	invalidateHomeCacheForSource(group.SourceId)
	return nil
}

func (s *HomeRecommendService) DeleteItem(ctx context.Context, itemID string) error {
	if err := repository.DeleteHomeRecommendItem(ctx, itemID); err != nil {
		return err
	}
	if primary := repository.CurrentPrimarySource(); primary != nil {
		invalidateHomeCacheForSource(primary.Id)
	}
	return nil
}

func (s *HomeRecommendService) ClearGroupItems(ctx context.Context, groupID string) error {
	group, err := repository.GetHomeRecommendGroup(ctx, groupID)
	if err != nil {
		return err
	}
	if group.Builtin {
		return errors.New("内置分组不支持清空")
	}
	if err := repository.ClearHomeRecommendGroupItems(ctx, group.Id); err != nil {
		return err
	}
	invalidateHomeCacheForSource(group.SourceId)
	return nil
}

// SyncGroupNow 立即根据分组绑定的豆瓣榜单同步一次本地片库数据。
func (s *HomeRecommendService) SyncGroupNow(ctx context.Context, groupID string) error {
	group, err := repository.GetHomeRecommendGroup(ctx, groupID)
	if err != nil {
		return err
	}
	if group.Builtin {
		return errors.New("内置分组不支持豆瓣同步")
	}
	chartKey := strings.TrimSpace(group.SyncChartKey)
	if !isValidChartKey(chartKey) {
		return errors.New("该分组未绑定有效的豆瓣/Bangumi榜单")
	}
	limit := int(group.SyncLimit)
	if limit <= 0 {
		limit = recommendSyncLimitDefault
	} else if limit > recommendSyncLimitMax {
		limit = recommendSyncLimitMax
	}

	cands, rates, err := HotChartsSvc.GetRawCandidatesByKey(chartKey, false)
	if err != nil {
		return err
	}
	version := filmsnapshot.GetActiveReadModelVersion()
	mids, midToCandIdx, err := filmsnapshot.MatchHotChartCandidates(version, group.SourceId, cands, limit)
	if err != nil {
		return err
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
	if err := repository.ReplaceHomeRecommendGroupItems(ctx, group.Id, group.SourceId, items, &now); err != nil {
		return err
	}
	invalidateHomeCacheForSource(group.SourceId)
	return nil
}

// SyncAllAutoGroups 定时同步所有开启 AutoSync 的推荐分组。
func (s *HomeRecommendService) SyncAllAutoGroups() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	groups, err := repository.ListAutoSyncRecommendGroups(ctx)
	if err != nil {
		log.Printf("[HomeRecommend] 查询自动同步分组失败: %v", err)
		return
	}
	for _, g := range groups {
		if err := s.SyncGroupNow(ctx, g.Id); err != nil {
			log.Printf("[HomeRecommend] 自动同步分组失败 group=%s (%s): %v", g.Id, g.Title, err)
		}
	}
}

// RecommendGenerateQuota 一键排片单个推荐分组的配额。
type RecommendGenerateQuota struct {
	GroupId string `json:"groupId"`
	Count   int    `json:"count"`
}

type RecommendGenerateReq struct {
	SourceId string                   `json:"sourceId"`
	Strategy string                   `json:"strategy"` // douban | latest | hot | score
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

// GenerateQuickPreview 一键排片推荐分组预览（支持 douban/latest/hot/score 四种策略）。
func (s *HomeRecommendService) GenerateQuickPreview(ctx context.Context, req RecommendGenerateReq) (RecommendGenerateResult, error) {
	src, _, _, err := resolveBannerSource(req.SourceId, true)
	if err != nil {
		return RecommendGenerateResult{}, err
	}
	strategy := strings.TrimSpace(req.Strategy)
	if strategy != "douban" && strategy != "latest" && strategy != "hot" && strategy != "score" {
		return RecommendGenerateResult{}, errors.New("排片策略只能是 douban、latest、hot 或 score")
	}

	allGroups, err := repository.ListHomeRecommendGroups(ctx, src.Id)
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

	version := filmsnapshot.GetActiveSnapshotVersion()
	readVersion := filmsnapshot.GetActiveReadModelVersion()
	tree, _ := repository.GetActiveCategoryTree(src.Id)

	out := RecommendGenerateResult{
		SourceId: src.Id,
		Strategy: strategy,
		Groups:   make([]RecommendGenerateGroupPreview, 0, len(req.Quotas)),
	}

	for _, q := range req.Quotas {
		g, ok := groupByID[strings.TrimSpace(q.GroupId)]
		if !ok || g.Builtin || q.Count <= 0 {
			continue
		}
		count := q.Count
		if count > recommendSyncLimitMax {
			count = recommendSyncLimitMax
		}

		previewItems := make([]model.HomeRecommendAdminItem, 0, count)
		if strategy == "douban" {
			var cands []filmsnapshot.HotChartCandidate
			var rates map[int]string
			if isValidChartKey(g.SyncChartKey) {
				cands, rates, _ = HotChartsSvc.GetRawCandidatesByKey(g.SyncChartKey, false)
			} else {
				cands, _ = HotChartsSvc.GetRawCandidatesByCategoryName(g.Title)
			}
			mids, midToIdx, mErr := filmsnapshot.MatchHotChartCandidates(readVersion, src.Id, cands, count)
			if mErr != nil {
				return RecommendGenerateResult{}, mErr
			}
			snaps, sErr := filmsnapshot.ListSnapshotsByMIDsForHotCharts(ctx, mids)
			if sErr != nil {
				return RecommendGenerateResult{}, sErr
			}
			snapByMid := make(map[int64]model.FilmListSnapshot, len(snaps))
			for _, sn := range snaps {
				snapByMid[sn.Mid] = sn
			}
			for i, mid := range mids {
				sn, found := snapByMid[mid]
				if !found {
					continue
				}
				rate := ""
				if rates != nil {
					if idx, exists := midToIdx[mid]; exists {
						rate = rates[idx]
					}
				}
				yr := ""
				if sn.Year > 0 {
					yr = fmt.Sprintf("%d", sn.Year)
				}
				previewItems = append(previewItems, model.HomeRecommendAdminItem{
					GroupId:     g.Id,
					SourceId:    src.Id,
					Mid:         sn.Mid,
					Sort:        int64(i + 1),
					DoubanScore: rate,
					Name:        sn.Name,
					Picture:     sn.Picture,
					Year:        yr,
					CName:       sn.CName,
					Area:        sn.Area,
					Remarks:     sn.Remarks,
					Score:       sn.Score,
				})
			}
		} else {
			// latest / hot / score：匹配对应分类下的本地候选
			catIDs := resolveRecommendCategoryIDs(tree, g.Title, g.SyncChartKey)
			snapStrategy := bannerStrategies[strategy]
			snaps, sErr := filmsnapshot.GetSnapshotBannerCandidates(ctx, version, snapStrategy, src.Id, catIDs, count, nil)
			if sErr != nil {
				return RecommendGenerateResult{}, sErr
			}
			for i, sn := range snaps {
				yr := ""
				if sn.Year > 0 {
					yr = fmt.Sprintf("%d", sn.Year)
				}
				previewItems = append(previewItems, model.HomeRecommendAdminItem{
					GroupId:  g.Id,
					SourceId: src.Id,
					Mid:      sn.Mid,
					Sort:     int64(i + 1),
					Name:     sn.Name,
					Picture:  sn.Picture,
					Year:     yr,
					CName:    sn.CName,
					Area:     sn.Area,
					Remarks:  sn.Remarks,
					Score:    sn.Score,
				})
			}
		}

		out.Groups = append(out.Groups, RecommendGenerateGroupPreview{
			GroupId: g.Id,
			Title:   g.Title,
			Items:   previewItems,
		})
	}
	return out, nil
}

func resolveRecommendCategoryIDs(tree model.CategoryTree, title, chartKey string) []int64 {
	kw := ""
	switch {
	case chartKey == "movie_showing" || chartKey == "movie_hot" || strings.Contains(title, "电影") || strings.Contains(title, "热映"):
		kw = "电影"
	case chartKey == "variety_hot" || strings.Contains(title, "综艺"):
		kw = "综艺"
	case chartKey == "bangumi_calendar" || strings.Contains(title, "动漫") || strings.Contains(title, "番"):
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
	src, _, _, err := resolveBannerSource(req.SourceId, true)
	if err != nil {
		return err
	}
	for _, gp := range req.Groups {
		g, err := repository.GetHomeRecommendGroup(ctx, gp.GroupId)
		if err != nil || g.SourceId != src.Id || g.Builtin {
			continue
		}
		items := make([]model.HomeRecommendItem, 0, len(gp.Items))
		seen := make(map[int64]bool, len(gp.Items))
		for _, it := range gp.Items {
			if it.Mid <= 0 || seen[it.Mid] {
				continue
			}
			seen[it.Mid] = true
			items = append(items, model.HomeRecommendItem{
				Mid:         it.Mid,
				DoubanScore: strings.TrimSpace(it.DoubanScore),
			})
		}
		if err := repository.ReplaceHomeRecommendGroupItems(ctx, g.Id, src.Id, items, nil); err != nil {
			return err
		}
	}
	invalidateHomeCacheForSource(src.Id)
	return nil
}

// HomeSections 构建前台首页展示的推荐分组（仅返回已启用且有可播影片的分组）。
func (s *HomeRecommendService) HomeSections(sourceID string) []model.HomeRecommendSection {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return []model.HomeRecommendSection{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	groups, err := repository.ListEnabledHomeRecommendGroups(ctx, sourceID)
	if err != nil || len(groups) == 0 {
		return []model.HomeRecommendSection{}
	}
	items, err := repository.ListHomeRecommendItemsBySource(ctx, sourceID)
	if err != nil || len(items) == 0 {
		return []model.HomeRecommendSection{}
	}

	mids := make([]int64, 0, len(items))
	for _, it := range items {
		if it.Mid > 0 {
			mids = append(mids, it.Mid)
		}
	}
	version := filmsnapshot.GetActiveReadModelVersion()
	byID, err := infosByMid(version, mids)
	if err != nil {
		return []model.HomeRecommendSection{}
	}

	itemsByGroup := make(map[string][]model.HomeRecommendItem)
	for _, it := range items {
		itemsByGroup[it.GroupId] = append(itemsByGroup[it.GroupId], it)
	}

	sections := make([]model.HomeRecommendSection, 0, len(groups))
	for _, g := range groups {
		if g.Builtin {
			continue
		}
		rawItems := itemsByGroup[g.Id]
		pubItems := make([]model.HomeRecommendPublicItem, 0, len(rawItems))
		rank := 0
		for _, raw := range rawItems {
			info, ok := byID[raw.Mid]
			if !ok {
				continue
			}
			rank++
			pubItems = append(pubItems, model.HomeRecommendPublicItem{
				MovieBasicInfo: info,
				DoubanScore:    raw.DoubanScore,
				Rank:           rank,
			})
		}
		if len(pubItems) > 0 {
			sections = append(sections, model.HomeRecommendSection{
				Id:           g.Id,
				Title:        g.Title,
				AutoSync:     g.AutoSync,
				SyncChartKey: g.SyncChartKey,
				Items:        pubItems,
			})
		}
	}
	return sections
}

// HomeLayoutSlot 前台首页板块的统一编排项（严格按后台分组排序与开关返回）。
type HomeLayoutSlot struct {
	Kind       string                      `json:"kind"` // "daily_updates" | "category" | "recommend"
	CategoryId string                      `json:"categoryId,omitempty"`
	Recommend  *model.HomeRecommendSection `json:"recommend,omitempty"`
}

// HomeLayout 构建前台首页所有板块的展示顺序与可见性。
func (s *HomeRecommendService) HomeLayout(sourceID string) []HomeLayoutSlot {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return []HomeLayoutSlot{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = s.EnsureBuiltinGroups(ctx, sourceID)
	groups, err := repository.ListEnabledHomeRecommendGroups(ctx, sourceID)
	if err != nil || len(groups) == 0 {
		return []HomeLayoutSlot{}
	}

	recSections := s.HomeSections(sourceID)
	recByID := make(map[string]model.HomeRecommendSection, len(recSections))
	for _, sec := range recSections {
		recByID[sec.Id] = sec
	}

	layout := make([]HomeLayoutSlot, 0, len(groups))
	for _, g := range groups {
		if g.Builtin {
			if g.BuiltinKey == "daily_updates" {
				layout = append(layout, HomeLayoutSlot{Kind: "daily_updates"})
				continue
			}
			if strings.HasPrefix(g.BuiltinKey, "cat:") {
				cidStr := strings.TrimPrefix(g.BuiltinKey, "cat:")
				if cid, pErr := strconv.ParseInt(cidStr, 10, 64); pErr == nil && cid > 0 {
					layout = append(layout, HomeLayoutSlot{
						Kind:       "category",
						CategoryId: fmt.Sprintf("%d", cid),
					})
				}
			}
			continue
		}
		if sec, ok := recByID[g.Id]; ok && len(sec.Items) > 0 {
			secCopy := sec
			layout = append(layout, HomeLayoutSlot{
				Kind:      "recommend",
				Recommend: &secCopy,
			})
		}
	}
	return layout
}
