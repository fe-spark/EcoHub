package repository

import (
	"fmt"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

type sourceCategoryPlacement struct {
	SourceTypeId       int64
	ParentSourceTypeId int64
	Name               string
	Sort               int
	Depth              int
}

type categoryTreeWalkNode struct {
	Node     *model.CategoryTree
	ParentId int64
	Depth    int
	Sort     int
}

func SaveCategoryTree(sourceId string, tree *model.CategoryTree) error {
	return saveCategoryTree(sourceId, tree, true, false)
}

func ResetCategoryTree(sourceId string, tree *model.CategoryTree) error {
	return saveCategoryTree(sourceId, tree, false, false)
}

func saveCategoryTree(sourceId string, tree *model.CategoryTree, preserveBusinessFields bool, skipRebuild bool) error {
	sourceId = strings.TrimSpace(sourceId)
	if sourceId == "" {
		return fmt.Errorf("source id 不能为空")
	}
	if tree == nil {
		return nil
	}

	plans := make([]sourceCategoryPlacement, 0)
	if err := flattenSourceCategoryPlacements(tree.Children, 0, 0, &plans); err != nil {
		return err
	}
	return saveCategoryPlans(sourceId, plans, preserveBusinessFields, skipRebuild)
}

func saveCategoryPlans(sourceId string, plans []sourceCategoryPlacement, preserveBusinessFields bool, skipRebuild bool) error {
	sourceId = strings.TrimSpace(sourceId)
	if sourceId == "" {
		return fmt.Errorf("source id 不能为空")
	}

	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		var oldCategories []model.Category
		if err := tx.Order("pid ASC, sort ASC, id ASC").Find(&oldCategories).Error; err != nil {
			return err
		}
		currentMap := make(map[int64]model.Category, len(oldCategories))
		for _, item := range oldCategories {
			currentMap[item.Id] = item
		}

		var oldMappings []model.CategoryMapping
		if err := tx.Where("source_id = ?", sourceId).Find(&oldMappings).Error; err != nil {
			return err
		}
		existingCategoryIDs := make(map[int64]struct{}, len(oldMappings))
		existingBySourceType := make(map[int64]int64, len(oldMappings))
		for _, item := range oldMappings {
			existingBySourceType[item.SourceTypeId] = item.CategoryId
			existingCategoryIDs[item.CategoryId] = struct{}{}
		}

		if !preserveBusinessFields {
			existingCategoryIDs = make(map[int64]struct{})
			existingBySourceType = make(map[int64]int64)
			if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.CategoryMapping{}).Error; err != nil {
				return err
			}
			if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.Category{}).Error; err != nil {
				return err
			}
			if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.SourceCategory{}).Error; err != nil {
				return err
			}
			if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.SearchTagItem{}).Error; err != nil {
				return err
			}
			currentMap = make(map[int64]model.Category)
		}

		if preserveBusinessFields {
			if err := tx.Where("source_id = ?", sourceId).Delete(&model.SourceCategory{}).Error; err != nil {
				return err
			}
		}
		rawRows := make([]model.SourceCategory, 0, len(plans))
		for _, plan := range plans {
			rawRows = append(rawRows, model.SourceCategory{
				SourceId:           sourceId,
				SourceTypeId:       plan.SourceTypeId,
				ParentSourceTypeId: plan.ParentSourceTypeId,
				RawName:            strings.TrimSpace(plan.Name),
				Sort:               plan.Sort,
				Depth:              plan.Depth,
			})
		}
		if len(rawRows) > 0 {
			if err := tx.Create(&rawRows).Error; err != nil {
				return err
			}
		}

		sourceTypeToCategory := make(map[int64]int64, len(plans))
		claimedCategoryIDs := make(map[int64]struct{}, len(plans))
		seenSourceType := make(map[int64]struct{}, len(plans))
		sharedCategoryIDs := make(map[int64]struct{})
		var sharedRows []int64
		if err := tx.Model(&model.CategoryMapping{}).
			Where("source_id <> ? AND category_id > 0", sourceId).
			Distinct("category_id").
			Pluck("category_id", &sharedRows).Error; err != nil {
			return err
		}
		for _, id := range sharedRows {
			sharedCategoryIDs[id] = struct{}{}
		}
		for _, plan := range plans {
			if _, ok := seenSourceType[plan.SourceTypeId]; ok {
				return fmt.Errorf("来源分类重复: %d", plan.SourceTypeId)
			}
			seenSourceType[plan.SourceTypeId] = struct{}{}
			name := strings.TrimSpace(plan.Name)
			if name == "" {
				return fmt.Errorf("来源分类名为空: %d", plan.SourceTypeId)
			}

			pid := int64(0)
			if plan.ParentSourceTypeId > 0 {
				parentId, ok := sourceTypeToCategory[plan.ParentSourceTypeId]
				if !ok {
					return fmt.Errorf("来源父分类不存在: %d", plan.ParentSourceTypeId)
				}
				pid = parentId
			}

			// 身份是本站 type_id，不按展示名并到其他站的分类。
			stableKey := fmt.Sprintf("source:%s:%d", sourceId, plan.SourceTypeId)
			categoryId := existingBySourceType[plan.SourceTypeId]
			if categoryId > 0 {
				if _, shared := sharedCategoryIDs[categoryId]; shared {
					categoryId = 0
				}
				if _, claimed := claimedCategoryIDs[categoryId]; claimed {
					categoryId = 0
				}
			}
			if categoryId > 0 {
				existingCategory, ok := currentMap[categoryId]
				if !ok {
					return fmt.Errorf("已有业务分类不存在: %d", categoryId)
				}
				updates := map[string]any{
					"pid":        pid,
					"name":       name,
					"stable_key": stableKey,
				}
				if !preserveBusinessFields {
					updates["sort"] = plan.Sort
					updates["show"] = true
					updates["alias"] = ""
				}
				if err := tx.Model(&model.Category{}).Where("id = ?", categoryId).Updates(updates).Error; err != nil {
					return err
				}
				existingCategory.Pid = pid
				existingCategory.Name = name
				existingCategory.StableKey = stableKey
				currentMap[categoryId] = existingCategory
			} else {
				category := model.Category{Pid: pid, Name: name, StableKey: stableKey, Show: true, Sort: plan.Sort}
				if err := tx.Create(&category).Error; err != nil {
					return err
				}
				categoryId = category.Id
				currentMap[categoryId] = category
			}
			claimedCategoryIDs[categoryId] = struct{}{}
			sourceTypeToCategory[plan.SourceTypeId] = categoryId
		}

		if preserveBusinessFields {
			if err := tx.Where("source_id = ?", sourceId).Delete(&model.CategoryMapping{}).Error; err != nil {
				return err
			}
		}
		mappings := make([]model.CategoryMapping, 0, len(plans))
		activeCategoryIDs := make(map[int64]struct{}, len(plans))
		for _, plan := range plans {
			categoryId := sourceTypeToCategory[plan.SourceTypeId]
			activeCategoryIDs[categoryId] = struct{}{}
			mappings = append(mappings, model.CategoryMapping{
				SourceId:     sourceId,
				SourceTypeId: plan.SourceTypeId,
				CategoryId:   categoryId,
			})
		}
		if len(mappings) > 0 {
			if err := tx.Create(&mappings).Error; err != nil {
				return err
			}
		}

		if preserveBusinessFields {
			staleCategoryIDs := make([]int64, 0)
			for categoryId := range existingCategoryIDs {
				if _, ok := activeCategoryIDs[categoryId]; ok {
					continue
				}
				if _, ok := claimedCategoryIDs[categoryId]; ok {
					continue
				}
				staleCategoryIDs = append(staleCategoryIDs, categoryId)
			}
			if err := dropStaleCategoriesTx(tx, sourceId, staleCategoryIDs, currentMap); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	if !skipRebuild {
		MarkCategoryChanged()
	}
	return nil
}

// dropStaleCategoriesTx 删掉只属于当前站、且这次同步不再使用的展示分类。
// 其他站的映射还指着的分类留着。
func dropStaleCategoriesTx(tx *gorm.DB, sourceId string, staleCategoryIDs []int64, currentMap map[int64]model.Category) error {
	if len(staleCategoryIDs) == 0 {
		return nil
	}
	var stillUsed []int64
	if err := tx.Model(&model.CategoryMapping{}).
		Where("source_id <> ? AND category_id IN ?", sourceId, staleCategoryIDs).
		Distinct("category_id").
		Pluck("category_id", &stillUsed).Error; err != nil {
		return err
	}
	used := make(map[int64]struct{}, len(stillUsed))
	for _, id := range stillUsed {
		used[id] = struct{}{}
	}
	removable := make([]int64, 0, len(staleCategoryIDs))
	for _, id := range staleCategoryIDs {
		if _, ok := used[id]; ok {
			continue
		}
		removable = append(removable, id)
	}
	if len(removable) == 0 {
		return nil
	}
	if err := tx.Where("id IN ?", removable).Delete(&model.Category{}).Error; err != nil {
		return err
	}
	for _, id := range removable {
		delete(currentMap, id)
	}
	return nil
}

func walkTwoLevelCategoryTree(nodes []*model.CategoryTree, parentId int64, depth int, visit func(item categoryTreeWalkNode) error) error {
	if len(nodes) == 0 {
		return nil
	}
	if depth > 1 {
		return fmt.Errorf("分类层级最多支持两层")
	}

	for index, node := range nodes {
		if err := visit(categoryTreeWalkNode{
			Node:     node,
			ParentId: parentId,
			Depth:    depth,
			Sort:     index + 1,
		}); err != nil {
			return err
		}
		if err := walkTwoLevelCategoryTree(node.Children, node.Id, depth+1, visit); err != nil {
			return err
		}
	}

	return nil
}

func flattenSourceCategoryPlacements(nodes []*model.CategoryTree, parentId int64, depth int, out *[]sourceCategoryPlacement) error {
	return walkTwoLevelCategoryTree(nodes, parentId, depth, func(item categoryTreeWalkNode) error {
		node := item.Node
		if node == nil || node.Id <= 0 {
			return fmt.Errorf("来源分类数据异常")
		}
		name := strings.TrimSpace(node.Name)
		if name == "" {
			return fmt.Errorf("来源分类名称不能为空")
		}
		*out = append(*out, sourceCategoryPlacement{
			SourceTypeId:       node.Id,
			ParentSourceTypeId: item.ParentId,
			Name:               name,
			Sort:               item.Sort,
			Depth:              item.Depth,
		})
		return nil
	})
}
