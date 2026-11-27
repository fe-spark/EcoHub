package recommend

import (
	"context"
	"errors"

	"server/internal/banner"
	"server/internal/film/catalog"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/model"
)

// RecommendCategoryOption 可绑定的站点可见一级分类选项。
type RecommendCategoryOption struct {
	Id        int64  `json:"id"`
	Name      string `json:"name"`
	FilmCount int64  `json:"filmCount,omitempty"`
}

// ImportCategoryGroupsResult 一键导入站点分类分组的返回摘要。
type ImportCategoryGroupsResult struct {
	CreatedCount int                        `json:"createdCount"`
	Groups       []model.HomeRecommendGroup `json:"groups"`
}

type categoryMidsSlot struct {
	movies []int64
	hot    []int64
}

func categoryHomeField(cat *model.CategoryTree) (string, int64) {
	if cat != nil && len(cat.Children) > 0 {
		return "pid", cat.Id
	}
	if cat != nil {
		return "cid", cat.Id
	}
	return "cid", 0
}

// visibleRootCategories 获取指定站点在「分类管理」中当前可见的一级分类列表与索引。
func visibleRootCategories(sourceID string) ([]*model.CategoryTree, map[int64]*model.CategoryTree) {
	tree, err := catalog.GetActiveCategoryTree(sourceID)
	if err != nil {
		return nil, map[int64]*model.CategoryTree{}
	}
	ordered := make([]*model.CategoryTree, 0, len(tree.Children))
	byID := make(map[int64]*model.CategoryTree, len(tree.Children))
	for _, ch := range tree.Children {
		if ch == nil || !ch.Show || ch.Id <= 0 {
			continue
		}
		ordered = append(ordered, ch)
		byID[ch.Id] = ch
	}
	return ordered, byID
}

// adminRootCategoryNames 读取指定站点的后台分类树名称映射（含已隐藏分类）。
func adminRootCategoryNames(sourceID string) map[int64]string {
	tree := catalog.GetSourceBoundCategoryTree(sourceID)
	out := make(map[int64]string, len(tree.Children))
	for _, ch := range tree.Children {
		if ch != nil && ch.Id > 0 {
			out[ch.Id] = ch.Name
		}
	}
	return out
}

func categoryOptionsFromTree(roots []*model.CategoryTree) []RecommendCategoryOption {
	opts := make([]RecommendCategoryOption, 0, len(roots))
	for _, ch := range roots {
		opts = append(opts, RecommendCategoryOption{
			Id:        ch.Id,
			Name:      ch.Name,
			FilmCount: ch.FilmCount,
		})
	}
	return opts
}

// collectCategorySlots 批量收集多个分类分组的影片 ID，并一次 SQL 加载全部基础信息避免 N+1 查询。
// 返回的 ok 为 false 表示加载过程中发生异常，首页不得缓存该降级结果。
func collectCategorySlots(ctx context.Context, version, sourceID string, groups []model.HomeRecommendGroup, visibleCats map[int64]*model.CategoryTree, withHot bool) (map[int64]categoryMidsSlot, map[int64]model.MovieBasicInfo, bool) {
	slots := make(map[int64]categoryMidsSlot)
	seenMids := make(map[int64]struct{})
	allMids := make([]int64, 0)
	okAll := true

	addMids := func(list []int64) {
		for _, mid := range list {
			if mid <= 0 {
				continue
			}
			if _, ok := seenMids[mid]; ok {
				continue
			}
			seenMids[mid] = struct{}{}
			allMids = append(allMids, mid)
		}
	}

	for _, g := range groups {
		if g.CategoryId <= 0 {
			continue
		}
		if _, done := slots[g.CategoryId]; done {
			continue
		}
		cat, ok := visibleCats[g.CategoryId]
		if !ok || !cat.Show || cat.Id <= 0 {
			continue
		}
		limit := 14
		if !withHot && g.SyncLimit > 0 {
			limit = int(clampSyncLimit(g.SyncLimit))
		}
		field, catID := categoryHomeField(cat)
		movies, hot, err := filmsnapshot.HomeCategoryMidsCtx(ctx, version, sourceID, field, catID, limit, 50)
		if err != nil {
			okAll = false
			continue
		}
		addMids(movies)
		if withHot {
			addMids(hot)
			slots[g.CategoryId] = categoryMidsSlot{movies: movies, hot: hot}
		} else {
			slots[g.CategoryId] = categoryMidsSlot{movies: movies}
		}
	}

	if len(allMids) == 0 {
		return slots, map[int64]model.MovieBasicInfo{}, okAll
	}
	byID, err := filmsnapshot.MovieBasicInfosByMIDsCtx(ctx, version, allMids)
	if err != nil {
		return slots, map[int64]model.MovieBasicInfo{}, false
	}
	return slots, byID, okAll
}

func buildCategoryPreviewAdminItems(sourceID string, cat *model.CategoryTree, slot categoryMidsSlot, byID map[int64]model.MovieBasicInfo, limit int) []model.HomeRecommendAdminItem {
	if cat == nil || len(slot.movies) == 0 {
		return []model.HomeRecommendAdminItem{}
	}
	mids := slot.movies
	if limit > 0 && len(mids) > limit {
		mids = mids[:limit]
	}
	items := make([]model.HomeRecommendAdminItem, 0, len(mids))
	for idx, mid := range mids {
		info, ok := byID[mid]
		if !ok {
			continue
		}
		items = append(items, model.HomeRecommendAdminItem{
			ItemId:   cat.Name,
			SourceId: sourceID,
			Mid:      mid,
			Sort:     int64(idx + 1),
			Name:     info.Name,
			Picture:  info.Picture,
			Year:     info.Year,
			CName:    info.CName,
			Area:     info.Area,
			Remarks:  info.Remarks,
		})
	}
	return items
}

func buildCategoryPublicSection(g model.HomeRecommendGroup, cat *model.CategoryTree, slot categoryMidsSlot, byID map[int64]model.MovieBasicInfo) (model.HomeRecommendSection, bool) {
	if cat == nil || !cat.Show || cat.Id <= 0 {
		return model.HomeRecommendSection{}, false
	}

	pubItems := make([]model.HomeRecommendPublicItem, 0, len(slot.movies))
	rank := 0
	for _, mid := range slot.movies {
		info, ok := byID[mid]
		if !ok {
			continue
		}
		rank++
		pubItems = append(pubItems, model.HomeRecommendPublicItem{
			MovieBasicInfo: info,
			Rank:           rank,
		})
	}
	hot := make([]model.MovieBasicInfo, 0, len(slot.hot))
	for _, mid := range slot.hot {
		if info, ok := byID[mid]; ok {
			hot = append(hot, info)
		}
	}
	if len(pubItems) == 0 && len(hot) == 0 {
		return model.HomeRecommendSection{}, false
	}

	title := g.Title
	if title == "" {
		title = cat.Name
	}
	return model.HomeRecommendSection{
		Id:         g.Id,
		Kind:       "category",
		Title:      title,
		CategoryId: cat.Id,
		Nav:        cat,
		Items:      pubItems,
		Hot:        hot,
	}, true
}

// ImportCategoryGroups 一键导入当前站点「分类管理」中显示的一级分类为推荐分组（事务内行锁判重防并发重复插入）。
func (s *HomeRecommendService) ImportCategoryGroups(ctx context.Context, sourceID string) (ImportCategoryGroupsResult, error) {
	src, _, _, err := banner.ResolveBannerSource(sourceID, true)
	if err != nil {
		return ImportCategoryGroupsResult{}, err
	}
	roots, _ := visibleRootCategories(src.Id)
	if len(roots) == 0 {
		return ImportCategoryGroupsResult{}, errors.New("当前采集站暂无已显示的一级分类")
	}
	created, err := CreateMissingGroupsAtomicTx(ctx, src.Id, func(existing []model.HomeRecommendGroup, maxSort int64) []model.HomeRecommendGroup {
		existByCat := make(map[int64]bool, len(existing))
		for _, g := range existing {
			if g.CategoryId > 0 {
				existByCat[g.CategoryId] = true
			}
		}
		out := make([]model.HomeRecommendGroup, 0, len(roots))
		for _, ch := range roots {
			if existByCat[ch.Id] {
				continue
			}
			maxSort++
			out = append(out, model.HomeRecommendGroup{
				SourceId:   src.Id,
				Title:      ch.Name,
				Sort:       maxSort,
				Enabled:    true,
				CategoryId: ch.Id,
				SyncLimit:  recommendSyncLimitDefault,
			})
		}
		return out
	})
	if err != nil {
		return ImportCategoryGroupsResult{}, err
	}
	invalidateHomeCacheForSource(src.Id)
	groups, err := ListHomeRecommendGroups(ctx, src.Id)
	if err != nil {
		return ImportCategoryGroupsResult{}, err
	}
	return ImportCategoryGroupsResult{
		CreatedCount: len(created),
		Groups:       groups,
	}, nil
}
