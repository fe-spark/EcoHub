package recommend

import (
	"context"
	"log"
	"strings"
	"time"

	filmsnapshot "server/internal/film/snapshot"
	"server/internal/model"
)

// HomeSections 构建前台首页统一展示楼层（仅返回已启用、未在分类管理隐藏且有可播影片的分组）。
// 返回值 cacheable 为 false 表示存在 DB/读模型超时或异常，调用方不得将该结果写入长 TTL 缓存。
func (s *HomeRecommendService) HomeSections(sourceID string) ([]model.HomeRecommendSection, bool) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return []model.HomeRecommendSection{}, true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	groups, err := ListEnabledHomeRecommendGroups(ctx, sourceID)
	if err != nil {
		log.Printf("[HomeRecommend] 读取首页推荐分组失败 source=%s: %v", sourceID, err)
		return []model.HomeRecommendSection{}, false
	}
	if len(groups) == 0 {
		return []model.HomeRecommendSection{}, true
	}

	cacheable := true
	hasRecommendGroup := false
	hasCategoryGroup := false
	for _, g := range groups {
		if g.CategoryId > 0 {
			hasCategoryGroup = true
		} else {
			hasRecommendGroup = true
		}
	}

	version := filmsnapshot.GetActiveReadModelVersion()
	var (
		itemsByGroup   map[string][]model.HomeRecommendItem
		byID           map[int64]model.MovieBasicInfo
		visiblePlayMID map[int64]bool
	)
	if hasRecommendGroup {
		items, itemErr := ListHomeRecommendItemsBySource(ctx, sourceID)
		if itemErr != nil {
			log.Printf("[HomeRecommend] 读取首页推荐条目失败 source=%s: %v", sourceID, itemErr)
			cacheable = false
		} else if len(items) > 0 {
			mids := make([]int64, 0, len(items))
			for _, it := range items {
				if it.Mid > 0 {
					mids = append(mids, it.Mid)
				}
			}
			if len(mids) > 0 {
				if vis, visErr := filmsnapshot.VisiblePlayableMIDs(ctx, sourceID, mids); visErr != nil {
					log.Printf("[HomeRecommend] 校验影片可见性失败 source=%s: %v", sourceID, visErr)
					cacheable = false
				} else if loadedByID, infoErr := filmsnapshot.MovieBasicInfosByMIDs(version, mids); infoErr != nil {
					log.Printf("[HomeRecommend] 读取推荐影片信息失败 source=%s: %v", sourceID, infoErr)
					cacheable = false
				} else {
					visiblePlayMID = vis
					byID = loadedByID
				}
			}
			itemsByGroup = make(map[string][]model.HomeRecommendItem)
			for _, it := range items {
				itemsByGroup[it.GroupId] = append(itemsByGroup[it.GroupId], it)
			}
		}
	}

	var (
		visibleCatMap map[int64]*model.CategoryTree
		catSlots      map[int64]categoryMidsSlot
		catInfos      map[int64]model.MovieBasicInfo
	)
	if hasCategoryGroup {
		_, visibleCatMap = visibleRootCategories(sourceID)
		var catOK bool
		catSlots, catInfos, catOK = collectCategorySlots(version, sourceID, groups, visibleCatMap, true)
		if !catOK {
			cacheable = false
		}
	}

	sections := make([]model.HomeRecommendSection, 0, len(groups))
	for _, g := range groups {
		if g.CategoryId > 0 {
			if sec, ok := buildCategoryPublicSection(g, visibleCatMap[g.CategoryId], catSlots[g.CategoryId], catInfos); ok {
				sections = append(sections, sec)
			}
			continue
		}

		if len(byID) == 0 || len(visiblePlayMID) == 0 {
			continue
		}
		rawItems := itemsByGroup[g.Id]
		pubItems := make([]model.HomeRecommendPublicItem, 0, len(rawItems))
		rank := 0
		for _, raw := range rawItems {
			if !visiblePlayMID[raw.Mid] {
				continue
			}
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
				Kind:         "recommend",
				Title:        g.Title,
				AutoSync:     g.AutoSync,
				SyncChartKey: g.SyncChartKey,
				Items:        pubItems,
			})
		}
	}

	return sections, cacheable
}
