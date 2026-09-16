package repository

import (
	"fmt"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"

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
		stableKeyToCategory := make(map[string]model.Category, len(oldCategories))
		categoryByParentName := make(map[string]model.Category, len(oldCategories))
		for _, item := range oldCategories {
			currentMap[item.Id] = item
			categoryByParentName[categoryParentNameKey(item.Pid, item.Name)] = item
			stableKey := strings.TrimSpace(item.StableKey)
			if stableKey != "" {
				stableKeyToCategory[stableKey] = item
			}
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
			stableKeyToCategory = make(map[string]model.Category)
			categoryByParentName = make(map[string]model.Category)
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
		sourceTypeToChildrenParentCategory := make(map[int64]int64, len(plans))
		sourceTypeToDisplayKey := make(map[int64]string, len(plans))
		claimedCategoryIDs := make(map[int64]struct{}, len(plans))
		seenSourceType := make(map[int64]struct{}, len(plans))
		for _, plan := range plans {
			if _, ok := seenSourceType[plan.SourceTypeId]; ok {
				return fmt.Errorf("来源分类重复: %d", plan.SourceTypeId)
			}
			seenSourceType[plan.SourceTypeId] = struct{}{}
			normalizedName := normalizeCategoryPlanName(plan)

			pid := int64(0)
			parentDisplayKey := sourceTypeToDisplayKey[plan.ParentSourceTypeId]
			if plan.ParentSourceTypeId > 0 {
				parentId, ok := sourceTypeToChildrenParentCategory[plan.ParentSourceTypeId]
				if !ok {
					return fmt.Errorf("来源父分类不存在: %d", plan.ParentSourceTypeId)
				}
				pid = parentId

				rawName := strings.TrimSpace(plan.Name)
				subName := support.NormalizeSubCategoryName(rawName)
				if subName != "" && subName != rawName {
					subStableKey := buildDisplayCategoryStableKey(pid, subName, parentDisplayKey)
					subCategory, err := ensureDisplayCategoryTx(tx, currentMap, stableKeyToCategory, categoryByParentName, pid, subName, subStableKey, plan.Sort, preserveBusinessFields)
					if err != nil {
						return err
					}
					claimedCategoryIDs[subCategory.Id] = struct{}{}
					pid = subCategory.Id
					parentDisplayKey = subCategory.StableKey
					normalizedName = rawName
				}
			} else {
				rawName := strings.TrimSpace(plan.Name)
				rootName := support.NormalizeRootCategoryName(rawName)
				if rootName != "" && rootName != rawName {
					rootStableKey := buildDisplayCategoryStableKey(0, rootName, "")
					rootCategory, err := ensureDisplayCategoryTx(tx, currentMap, stableKeyToCategory, categoryByParentName, 0, rootName, rootStableKey, plan.Sort, preserveBusinessFields)
					if err != nil {
						return err
					}
					claimedCategoryIDs[rootCategory.Id] = struct{}{}
					pid = rootCategory.Id
					parentDisplayKey = rootCategory.StableKey
					normalizedName = rawName
				}
			}

			stableKey := buildDisplayCategoryStableKey(pid, normalizedName, parentDisplayKey)
			if stableKey == "" {
				return fmt.Errorf("来源分类稳定标识生成失败: %d", plan.SourceTypeId)
			}

			if existingCategory, ok := categoryByParentName[categoryParentNameKey(pid, normalizedName)]; ok {
				updates := map[string]any{
					"pid":        pid,
					"name":       normalizedName,
					"stable_key": stableKey,
				}
				if !preserveBusinessFields {
					updates["sort"] = plan.Sort
					updates["show"] = true
					updates["alias"] = ""
				}
				if err := tx.Model(&model.Category{}).Where("id = ?", existingCategory.Id).Updates(updates).Error; err != nil {
					return err
				}
				existingCategory.Pid = pid
				existingCategory.Name = normalizedName
				existingCategory.StableKey = stableKey
				currentMap[existingCategory.Id] = existingCategory
				stableKeyToCategory[stableKey] = existingCategory
				categoryByParentName[categoryParentNameKey(pid, normalizedName)] = existingCategory
				sourceTypeToCategory[plan.SourceTypeId] = existingCategory.Id
				sourceTypeToDisplayKey[plan.SourceTypeId] = stableKey
				claimedCategoryIDs[existingCategory.Id] = struct{}{}
				if plan.ParentSourceTypeId == 0 && pid > 0 {
					sourceTypeToChildrenParentCategory[plan.SourceTypeId] = pid
				} else {
					sourceTypeToChildrenParentCategory[plan.SourceTypeId] = existingCategory.Id
				}
				continue
			}

			if existingCategory, ok := stableKeyToCategory[stableKey]; ok {
				sourceTypeToCategory[plan.SourceTypeId] = existingCategory.Id
				sourceTypeToDisplayKey[plan.SourceTypeId] = stableKey
				claimedCategoryIDs[existingCategory.Id] = struct{}{}
				if plan.ParentSourceTypeId == 0 && pid > 0 {
					sourceTypeToChildrenParentCategory[plan.SourceTypeId] = pid
				} else {
					sourceTypeToChildrenParentCategory[plan.SourceTypeId] = existingCategory.Id
				}
				continue
			}

			categoryId := existingBySourceType[plan.SourceTypeId]
			if categoryId > 0 {
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
					"name":       normalizedName,
					"stable_key": stableKey,
				}
				if preserveBusinessFields {
					updates["sort"] = existingCategory.Sort
				} else {
					updates["sort"] = plan.Sort
					updates["show"] = true
					updates["alias"] = ""
				}
				if err := tx.Model(&model.Category{}).Where("id = ?", categoryId).Updates(updates).Error; err != nil {
					return err
				}
				existingCategory.Pid = pid
				existingCategory.Name = normalizedName
				existingCategory.StableKey = stableKey
				currentMap[categoryId] = existingCategory
				stableKeyToCategory[stableKey] = existingCategory
				categoryByParentName[categoryParentNameKey(pid, normalizedName)] = existingCategory
			} else {
				category := model.Category{Pid: pid, Name: normalizedName, StableKey: stableKey, Show: true, Sort: plan.Sort}
				if err := tx.Create(&category).Error; err != nil {
					return err
				}
				categoryId = category.Id
				currentMap[categoryId] = category
				stableKeyToCategory[stableKey] = category
				categoryByParentName[categoryParentNameKey(pid, normalizedName)] = category
			}
			claimedCategoryIDs[categoryId] = struct{}{}
			sourceTypeToCategory[plan.SourceTypeId] = categoryId
			sourceTypeToDisplayKey[plan.SourceTypeId] = stableKey
			if plan.ParentSourceTypeId == 0 && pid > 0 {
				sourceTypeToChildrenParentCategory[plan.SourceTypeId] = pid
			} else {
				sourceTypeToChildrenParentCategory[plan.SourceTypeId] = categoryId
			}
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
			if len(staleCategoryIDs) > 0 {
				if err := tx.Where("id IN ?", staleCategoryIDs).Delete(&model.Category{}).Error; err != nil {
					return err
				}
				for _, categoryId := range staleCategoryIDs {
					delete(currentMap, categoryId)
				}
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

func normalizeCategoryPlanName(plan sourceCategoryPlacement) string {
	name := strings.TrimSpace(plan.Name)
	if name == "" {
		return ""
	}
	if plan.ParentSourceTypeId == 0 {
		return support.NormalizeRootCategoryName(name)
	}
	return support.NormalizeSubCategoryName(name)
}

func categoryParentNameKey(pid int64, name string) string {
	return fmt.Sprintf("%d:%s", pid, strings.TrimSpace(name))
}

func ensureDisplayCategoryTx(tx *gorm.DB, currentMap map[int64]model.Category, stableKeyToCategory map[string]model.Category, categoryByParentName map[string]model.Category, pid int64, name string, stableKey string, sort int, preserveBusinessFields bool) (model.Category, error) {
	if category, ok := categoryByParentName[categoryParentNameKey(pid, name)]; ok {
		updates := map[string]any{
			"pid":        pid,
			"name":       name,
			"stable_key": stableKey,
		}
		if !preserveBusinessFields {
			updates["sort"] = sort
			updates["show"] = true
			updates["alias"] = ""
		}
		if err := tx.Model(&model.Category{}).Where("id = ?", category.Id).Updates(updates).Error; err != nil {
			return model.Category{}, err
		}
		category.Pid = pid
		category.Name = name
		category.StableKey = stableKey
		currentMap[category.Id] = category
		stableKeyToCategory[stableKey] = category
		categoryByParentName[categoryParentNameKey(pid, name)] = category
		return category, nil
	}

	if category, ok := stableKeyToCategory[stableKey]; ok {
		return category, nil
	}

	category := model.Category{Pid: pid, Name: name, StableKey: stableKey, Show: true, Sort: sort}
	if err := tx.Create(&category).Error; err != nil {
		return model.Category{}, err
	}
	currentMap[category.Id] = category
	stableKeyToCategory[stableKey] = category
	categoryByParentName[categoryParentNameKey(pid, name)] = category
	return category, nil
}

func buildDisplayCategoryStableKey(pid int64, name string, parentKey string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if pid == 0 {
		return fmt.Sprintf("display:root:%s", name)
	}
	parentKey = strings.TrimSpace(parentKey)
	if parentKey == "" {
		parentKey = support.GetCategoryStableKeyByID(pid)
	}
	if parentKey == "" {
		return fmt.Sprintf("display:sub:%d:%s", pid, name)
	}
	return fmt.Sprintf("%s/%s", parentKey, name)
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
