package recommend

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"server/internal/banner"
	"server/internal/config"
	"server/internal/film"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repobase"
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

func invalidateHomeCacheForSource(sourceID string) {
	repobase.BumpIndexPageGeneration()
	if db.Rdb == nil {
		return
	}
	version := filmsnapshot.GetActiveReadModelVersion()
	cacheKey := fmt.Sprintf("%s:v%s:src_%s:home:v2", config.IndexPageCacheKey, version, strings.TrimSpace(sourceID))
	_ = db.Rdb.Del(db.Cxt, cacheKey).Err()
}

// HomeRecommendListResult 后台列表返回。
type HomeRecommendListResult struct {
	SourceId        string                          `json:"sourceId"`
	PrimarySourceId string                          `json:"primarySourceId"`
	Sources         []banner.BannerSourceOption     `json:"sources"`
	ChartOptions    []ChartOption                   `json:"chartOptions"`
	CategoryOptions []RecommendCategoryOption       `json:"categoryOptions"`
	Groups          []model.HomeRecommendAdminGroup `json:"groups"`
}

// ListAdmin 获取指定采集站的后台推荐分组及影片列表。
func (s *HomeRecommendService) ListAdmin(ctx context.Context, sourceID string) (HomeRecommendListResult, error) {
	src, sources, primaryID, err := banner.ResolveBannerSource(sourceID, true)
	if err != nil {
		return HomeRecommendListResult{}, err
	}
	if err := s.ensureDefaultCategoryGroups(ctx, src.Id); err != nil {
		return HomeRecommendListResult{}, err
	}
	groups, err := ListHomeRecommendGroups(ctx, src.Id)
	if err != nil {
		return HomeRecommendListResult{}, err
	}
	items, err := ListHomeRecommendItemsBySource(ctx, src.Id)
	if err != nil {
		return HomeRecommendListResult{}, err
	}

	mids := make([]int64, 0, len(items))
	for _, it := range items {
		if it.Mid > 0 {
			mids = append(mids, it.Mid)
		}
	}
	liveByMid, err := film.LiveBannerSnapshotsByMIDs(ctx, src.Id, mids)
	if err != nil {
		return HomeRecommendListResult{}, err
	}
	vis := filmsnapshot.SourceCategoryVisibility(src.Id)

	itemsByGroup := make(map[string][]model.HomeRecommendAdminItem)
	for _, it := range items {
		sn, ok := liveByMid[it.Mid]
		catHidden := ok && !vis(sn.CategoryKey, sn.RootCategoryKey, sn.Cid, sn.Pid)
		adminItem := model.HomeRecommendAdminItem{
			ItemId:         it.Id,
			GroupId:        it.GroupId,
			SourceId:       it.SourceId,
			Mid:            it.Mid,
			Sort:           it.Sort,
			DoubanScore:    it.DoubanScore,
			Unavailable:    !ok || catHidden,
			CategoryHidden: catHidden,
		}
		if ok {
			adminItem.Name = sn.Name
			adminItem.Picture = sn.DisplayPicture()
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

	visibleRoots, visibleCatMap := visibleRootCategories(src.Id)
	adminCatNames := adminRootCategoryNames(src.Id)
	catSlots, catInfos, _ := collectCategorySlots(ctx, filmsnapshot.GetActiveReadModelVersion(), src.Id, groups, visibleCatMap, false)

	out := make([]model.HomeRecommendAdminGroup, 0, len(groups))
	for _, g := range groups {
		var gItems []model.HomeRecommendAdminItem
		catName := ""
		catHidden := false
		if g.CategoryId > 0 {
			if visCat, ok := visibleCatMap[g.CategoryId]; ok && visCat.Show {
				catName = visCat.Name
				gItems = buildCategoryPreviewAdminItems(src.Id, visCat, catSlots[g.CategoryId], catInfos, int(g.SyncLimit))
			} else {
				catHidden = true
				catName = adminCatNames[g.CategoryId]
				gItems = make([]model.HomeRecommendAdminItem, 0)
			}
		} else {
			gItems = itemsByGroup[g.Id]
			if gItems == nil {
				gItems = make([]model.HomeRecommendAdminItem, 0)
			}
		}
		out = append(out, model.HomeRecommendAdminGroup{
			HomeRecommendGroup: g,
			CategoryName:       catName,
			CategoryHidden:     catHidden,
			Items:              gItems,
		})
	}

	return HomeRecommendListResult{
		SourceId:        src.Id,
		PrimarySourceId: primaryID,
		Sources:         sources,
		ChartOptions:    RecommendChartOptions,
		CategoryOptions: categoryOptionsFromTree(visibleRoots),
		Groups:          out,
	}, nil
}

// InitPresetGroupsResult 导入预设分组及首轮同步摘要。
type InitPresetGroupsResult struct {
	Groups      []model.HomeRecommendGroup `json:"groups"`
	SyncSummary RecommendBatchSyncResult   `json:"syncSummary"`
}

// InitPresetGroups 为指定采集站初始化 5 个默认推荐分组（事务内行锁判重防并发重复插入），并反馈首轮同步统计。
func (s *HomeRecommendService) InitPresetGroups(ctx context.Context, sourceID string) (InitPresetGroupsResult, error) {
	src, _, _, err := banner.ResolveBannerSource(sourceID, true)
	if err != nil {
		return InitPresetGroupsResult{}, err
	}
	createdGroups, err := CreateMissingGroupsAtomicTx(ctx, src.Id, func(existing []model.HomeRecommendGroup, maxSort int64) []model.HomeRecommendGroup {
		existByChart := make(map[string]bool, len(existing))
		existByTitle := make(map[string]bool, len(existing))
		for _, g := range existing {
			if g.SyncChartKey != "" {
				existByChart[g.SyncChartKey] = true
			}
			existByTitle[g.Title] = true
		}
		out := make([]model.HomeRecommendGroup, 0, len(RecommendChartOptions))
		for _, opt := range RecommendChartOptions {
			if existByChart[opt.Key] || existByTitle[opt.Title] {
				continue
			}
			maxSort++
			out = append(out, model.HomeRecommendGroup{
				SourceId:     src.Id,
				Title:        opt.Title,
				Sort:         maxSort,
				Enabled:      true,
				AutoSync:     true,
				SyncChartKey: opt.Key,
				SyncLimit:    recommendSyncLimitDefault,
			})
		}
		return out
	})
	if err != nil {
		return InitPresetGroupsResult{}, err
	}
	summary := s.syncGroupList(ctx, createdGroups)
	invalidateHomeCacheForSource(src.Id)
	groups, err := ListHomeRecommendGroups(ctx, src.Id)
	if err != nil {
		return InitPresetGroupsResult{}, err
	}
	return InitPresetGroupsResult{
		Groups:      groups,
		SyncSummary: summary,
	}, nil
}

type RecommendGroupInput struct {
	SourceId     string  `json:"sourceId"`
	Title        string  `json:"title"`
	Enabled      *bool   `json:"enabled"`
	AutoSync     *bool   `json:"autoSync"`
	SyncChartKey *string `json:"syncChartKey"`
	CategoryId   *int64  `json:"categoryId"`
	SyncLimit    int64   `json:"syncLimit"`
}

func (s *HomeRecommendService) CreateGroup(ctx context.Context, req RecommendGroupInput) (model.HomeRecommendGroup, error) {
	src, _, _, err := banner.ResolveBannerSource(req.SourceId, true)
	if err != nil {
		return model.HomeRecommendGroup{}, err
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	chartKey := ""
	if req.SyncChartKey != nil {
		chartKey = strings.TrimSpace(*req.SyncChartKey)
	}
	categoryID := int64(0)
	if req.CategoryId != nil && *req.CategoryId > 0 {
		categoryID = *req.CategoryId
	}
	if chartKey != "" && categoryID > 0 {
		return model.HomeRecommendGroup{}, errors.New("榜单同步与站点分类不能同时绑定")
	}
	if chartKey != "" && !isValidChartKey(chartKey) {
		return model.HomeRecommendGroup{}, errors.New("请选择有效的热门榜单类型")
	}
	title := strings.TrimSpace(req.Title)
	if categoryID > 0 {
		_, visibleCatMap := visibleRootCategories(src.Id)
		cat, ok := visibleCatMap[categoryID]
		if !ok || !cat.Show {
			return model.HomeRecommendGroup{}, errors.New("请选择当前站点显示中的一级分类")
		}
		if title == "" {
			title = cat.Name
		}
	}
	if title == "" {
		return model.HomeRecommendGroup{}, errors.New("分组名称不能为空")
	}
	autoSync := false
	if req.AutoSync != nil {
		autoSync = *req.AutoSync
	}
	if chartKey == "" {
		if autoSync {
			return model.HomeRecommendGroup{}, errors.New("开启自动跟随榜单时，请选择对应榜单")
		}
		autoSync = false
	}
	limit := clampSyncLimit(req.SyncLimit)

	created, err := CreateHomeRecommendGroup(ctx, model.HomeRecommendGroup{
		SourceId:     src.Id,
		Title:        title,
		Enabled:      enabled,
		AutoSync:     autoSync,
		SyncChartKey: chartKey,
		CategoryId:   categoryID,
		SyncLimit:    limit,
	})
	if err != nil {
		return model.HomeRecommendGroup{}, err
	}
	if created.SyncChartKey != "" {
		_, _ = s.SyncGroupNow(ctx, created.Id)
	}
	_ = MarkSourceHomeRecommendInited(ctx, src.Id)
	invalidateHomeCacheForSource(src.Id)
	return created, nil
}

func clampSyncLimit(limit int64) int64 {
	if limit <= 0 {
		return recommendSyncLimitDefault
	}
	if limit > recommendSyncLimitMax {
		return recommendSyncLimitMax
	}
	return limit
}

func (s *HomeRecommendService) UpdateGroup(ctx context.Context, id string, req RecommendGroupInput) error {
	updated, syncNow, err := UpdateHomeRecommendGroupAtomic(ctx, id, func(existing model.HomeRecommendGroup) (model.HomeRecommendGroup, bool, bool, error) {
		prevCatID := existing.CategoryId
		prevChartKey := existing.SyncChartKey
		if strings.TrimSpace(req.Title) != "" {
			existing.Title = strings.TrimSpace(req.Title)
		}
		if req.Enabled != nil {
			existing.Enabled = *req.Enabled
		}
		if req.SyncChartKey != nil {
			chartKey := strings.TrimSpace(*req.SyncChartKey)
			if chartKey != "" && !isValidChartKey(chartKey) {
				return model.HomeRecommendGroup{}, false, false, errors.New("请选择有效的热门榜单类型")
			}
			existing.SyncChartKey = chartKey
			if chartKey != "" {
				existing.CategoryId = 0
			}
		}
		if req.CategoryId != nil {
			catID := *req.CategoryId
			if catID < 0 {
				catID = 0
			}
			if catID > 0 {
				if existing.SyncChartKey != "" {
					return model.HomeRecommendGroup{}, false, false, errors.New("榜单同步与站点分类不能同时绑定")
				}
				if catID != prevCatID {
					_, visibleCatMap := visibleRootCategories(existing.SourceId)
					if cat, ok := visibleCatMap[catID]; !ok || !cat.Show {
						return model.HomeRecommendGroup{}, false, false, errors.New("请选择当前站点显示中的一级分类")
					}
				}
			}
			existing.CategoryId = catID
		}
		if req.AutoSync != nil {
			existing.AutoSync = *req.AutoSync
		}
		if existing.CategoryId > 0 || existing.SyncChartKey == "" {
			if req.AutoSync != nil && *req.AutoSync {
				return model.HomeRecommendGroup{}, false, false, errors.New("开启自动跟随榜单时，请选择对应榜单")
			}
			existing.AutoSync = false
		}
		if req.SyncLimit > 0 {
			existing.SyncLimit = clampSyncLimit(req.SyncLimit)
		}
		catChanged := existing.CategoryId > 0 && existing.CategoryId != prevCatID
		chartChanged := existing.SyncChartKey != "" && existing.SyncChartKey != prevChartKey
		return existing, catChanged || chartChanged, chartChanged, nil
	})
	if err != nil {
		return err
	}
	if syncNow {
		_, _ = s.SyncGroupNow(ctx, updated.Id)
	}
	invalidateHomeCacheForSource(updated.SourceId)
	return nil
}

func (s *HomeRecommendService) DeleteGroup(ctx context.Context, id string) error {
	existing, err := GetHomeRecommendGroup(ctx, id)
	if err != nil {
		return err
	}
	if err := DeleteHomeRecommendGroup(ctx, id); err != nil {
		return err
	}
	invalidateHomeCacheForSource(existing.SourceId)
	return nil
}

func (s *HomeRecommendService) ReorderGroups(ctx context.Context, sourceID string, ids []string) error {
	src, _, _, err := banner.ResolveBannerSource(sourceID, true)
	if err != nil {
		return err
	}
	if err := ReorderHomeRecommendGroups(ctx, src.Id, ids); err != nil {
		return err
	}
	invalidateHomeCacheForSource(src.Id)
	return nil
}

func (s *HomeRecommendService) AddItem(ctx context.Context, groupID string, mid int64, doubanScore string) (model.HomeRecommendItem, error) {
	if mid <= 0 {
		return model.HomeRecommendItem{}, errors.New("影片编号不合法")
	}
	group, err := GetHomeRecommendGroup(ctx, groupID)
	if err != nil {
		return model.HomeRecommendItem{}, err
	}
	if group.CategoryId > 0 {
		return model.HomeRecommendItem{}, errors.New("站点分类分组由片库分类实时驱动，不支持手工添加影片")
	}
	visible, err := filmsnapshot.VisiblePlayableMIDs(ctx, group.SourceId, []int64{mid})
	if err != nil {
		return model.HomeRecommendItem{}, err
	}
	if !visible[mid] {
		return model.HomeRecommendItem{}, errors.New("影片不存在、不属于该采集站或所属分类已隐藏")
	}
	item, err := AddHomeRecommendItem(ctx, group.Id, group.SourceId, mid, doubanScore)
	if err != nil {
		return model.HomeRecommendItem{}, err
	}
	invalidateHomeCacheForSource(group.SourceId)
	return item, nil
}

func (s *HomeRecommendService) ReorderItems(ctx context.Context, groupID string, itemIDs []string) error {
	group, err := GetHomeRecommendGroup(ctx, groupID)
	if err != nil {
		return err
	}
	if err := ReorderHomeRecommendItems(ctx, group.Id, itemIDs); err != nil {
		return err
	}
	invalidateHomeCacheForSource(group.SourceId)
	return nil
}

func (s *HomeRecommendService) DeleteItem(ctx context.Context, itemID string) error {
	sourceID, err := DeleteHomeRecommendItem(ctx, itemID)
	if err != nil {
		return err
	}
	if sourceID != "" {
		invalidateHomeCacheForSource(sourceID)
	}
	return nil
}

func (s *HomeRecommendService) ClearGroupItems(ctx context.Context, groupID string) error {
	group, err := GetHomeRecommendGroup(ctx, groupID)
	if err != nil {
		return err
	}
	if group.CategoryId > 0 {
		return errors.New("站点分类分组由片库分类实时驱动，无需清空影片")
	}
	if err := ClearHomeRecommendGroupItems(ctx, group.Id); err != nil {
		return err
	}
	invalidateHomeCacheForSource(group.SourceId)
	return nil
}
