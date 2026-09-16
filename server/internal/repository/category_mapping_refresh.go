package repository

import (
	"fmt"
	"strings"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"
)

func loadSourceCategoryPlacementsBySourceIDs(sourceIDs []string) (map[string][]sourceCategoryPlacement, error) {
	if len(sourceIDs) == 0 {
		return map[string][]sourceCategoryPlacement{}, nil
	}

	var rows []model.SourceCategory
	if err := db.Mdb.Where("source_id IN ?", sourceIDs).
		Order("source_id ASC, depth ASC, parent_source_type_id ASC, sort ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	plansBySource := make(map[string][]sourceCategoryPlacement, len(sourceIDs))
	for _, row := range rows {
		sourceID := strings.TrimSpace(row.SourceId)
		if sourceID == "" {
			continue
		}
		name := strings.TrimSpace(row.RawName)
		if name == "" {
			return nil, fmt.Errorf("来源分类名称不能为空: %d", row.SourceTypeId)
		}
		plansBySource[sourceID] = append(plansBySource[sourceID], sourceCategoryPlacement{
			SourceTypeId:       row.SourceTypeId,
			ParentSourceTypeId: row.ParentSourceTypeId,
			Name:               name,
			Sort:               row.Sort,
			Depth:              row.Depth,
		})
	}
	return plansBySource, nil
}

func RefreshFutureCategoryMappingsFromSourceCategories() error {
	// 这里只刷新 categories/category_mappings/cacheSourceMap，不回写资源数据。
	// 已采集影片在查询时通过最新来源映射自然归入当前展示分组。
	var sourceIDs []string
	if err := db.Mdb.Model(&model.FilmSource{}).Where("state = ? AND grade = ?", true, model.MasterCollect).Pluck("id", &sourceIDs).Error; err != nil {
		return err
	}
	plansBySource, err := loadSourceCategoryPlacementsBySourceIDs(sourceIDs)
	if err != nil {
		return err
	}
	for _, sourceID := range sourceIDs {
		plans := plansBySource[sourceID]
		if len(plans) == 0 {
			continue
		}
		if err := saveCategoryPlans(sourceID, plans, true, true); err != nil {
			return err
		}
	}
	if err := deleteOrphanDisplayRootCategories(); err != nil {
		return err
	}
	RefreshCategoryCache()
	ReloadMappingRules()
	touchCategoryVersion()
	support.TouchSearchTagsVersion()
	if db.Rdb != nil {
		db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)
		db.Rdb.Del(db.Cxt, config.TVBoxConfigCacheKey)
	}
	ClearIndexPageCache()
	clearProvideListCache()
	return nil
}

func deleteOrphanDisplayRootCategories() error {
	var categories []model.Category
	if err := db.Mdb.Order("pid ASC, id ASC").Find(&categories).Error; err != nil {
		return err
	}

	childCountByPid := make(map[int64]int)
	for _, category := range categories {
		if category.Pid > 0 {
			childCountByPid[category.Pid]++
		}
	}

	var mappedCategoryIDs []int64
	if err := db.Mdb.Model(&model.CategoryMapping{}).Distinct("category_id").Pluck("category_id", &mappedCategoryIDs).Error; err != nil {
		return err
	}
	mapped := make(map[int64]struct{}, len(mappedCategoryIDs))
	for _, id := range mappedCategoryIDs {
		mapped[id] = struct{}{}
	}

	orphanIDs := make([]int64, 0)
	for _, category := range categories {
		if category.Pid != 0 {
			continue
		}
		if childCountByPid[category.Id] > 0 {
			continue
		}
		if _, ok := mapped[category.Id]; ok {
			continue
		}
		orphanIDs = append(orphanIDs, category.Id)
	}
	if len(orphanIDs) == 0 {
		return nil
	}
	return db.Mdb.Where("id IN ?", orphanIDs).Delete(&model.Category{}).Error
}

func clearProvideListCache() {
	if db.Rdb == nil {
		return
	}
	iter := db.Rdb.Scan(db.Cxt, 0, config.TVBoxList+":*", config.MaxScanCount).Iterator()
	for iter.Next(db.Cxt) {
		db.Rdb.Del(db.Cxt, iter.Val())
	}
}
