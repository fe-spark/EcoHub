package repository

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// CategoryApplyResult 存储分类树应用与数据清理的结果
type CategoryApplyResult struct {
	DataCleared  bool
	OrphanMids   []int64
	DetachedMids []int64
}

// DiffSourceCategoryTree 对比采集站现有原始分类树与最新拉取到的分类树计划
func DiffSourceCategoryTree(old []model.SourceCategory, plans []sourceCategoryPlacement) []model.CategoryTreeChange {
	oldMap := make(map[int64]model.SourceCategory, len(old))
	oldNameMap := make(map[int64]string, len(old))
	for _, item := range old {
		oldMap[item.SourceTypeId] = item
		oldNameMap[item.SourceTypeId] = item.RawName
	}

	newMap := make(map[int64]sourceCategoryPlacement, len(plans))
	newNameMap := make(map[int64]string, len(plans))
	for _, plan := range plans {
		newMap[plan.SourceTypeId] = plan
		newNameMap[plan.SourceTypeId] = plan.Name
	}

	allTypeIDsMap := make(map[int64]struct{}, len(old)+len(plans))
	for _, item := range old {
		allTypeIDsMap[item.SourceTypeId] = struct{}{}
	}
	for _, plan := range plans {
		allTypeIDsMap[plan.SourceTypeId] = struct{}{}
	}

	sortedTypeIDs := make([]int64, 0, len(allTypeIDsMap))
	for id := range allTypeIDsMap {
		sortedTypeIDs = append(sortedTypeIDs, id)
	}
	sort.Slice(sortedTypeIDs, func(i, j int) bool {
		return sortedTypeIDs[i] < sortedTypeIDs[j]
	})

	changes := make([]model.CategoryTreeChange, 0)
	for _, typeId := range sortedTypeIDs {
		oldItem, inOld := oldMap[typeId]
		newItem, inNew := newMap[typeId]

		if inOld && inNew {
			oldName := strings.TrimSpace(oldItem.RawName)
			newName := strings.TrimSpace(newItem.Name)

			// 1. 同一 id 先改名
			if oldName != newName {
				changes = append(changes, model.CategoryTreeChange{
					TypeId: typeId,
					Kind:   model.CategoryChangeRenamed,
					From:   oldName,
					To:     newName,
				})
			}

			// 2. 再换父级
			if oldItem.ParentSourceTypeId != newItem.ParentSourceTypeId {
				oldParentName := "顶级"
				if oldItem.ParentSourceTypeId > 0 {
					if p, ok := oldNameMap[oldItem.ParentSourceTypeId]; ok && strings.TrimSpace(p) != "" {
						oldParentName = strings.TrimSpace(p)
					}
				}
				newParentName := "顶级"
				if newItem.ParentSourceTypeId > 0 {
					if p, ok := newNameMap[newItem.ParentSourceTypeId]; ok && strings.TrimSpace(p) != "" {
						newParentName = strings.TrimSpace(p)
					}
				}
				changes = append(changes, model.CategoryTreeChange{
					TypeId: typeId,
					Kind:   model.CategoryChangeMoved,
					Name:   newName,
					From:   oldParentName,
					To:     newParentName,
				})
			}
		} else if inOld && !inNew {
			// 已有分类在新树中被删除
			changes = append(changes, model.CategoryTreeChange{
				TypeId: typeId,
				Kind:   model.CategoryChangeRemoved,
				Name:   strings.TrimSpace(oldItem.RawName),
			})
		}
		// 仅新增分类不产生变更项
	}

	return changes
}

// PreviewSourceCategoryChanges 预览分类树变更，若返回错误则中止同步
func PreviewSourceCategoryChanges(sourceId string, tree *model.CategoryTree) ([]model.CategoryTreeChange, error) {
	sourceId = strings.TrimSpace(sourceId)
	if sourceId == "" {
		return nil, errors.New("source id 不能为空")
	}
	if db.Mdb == nil {
		return nil, errors.New("数据库未初始化")
	}

	plans := make([]sourceCategoryPlacement, 0)
	if tree != nil {
		if err := flattenSourceCategoryPlacements(tree.Children, 0, 0, &plans); err != nil {
			return nil, err
		}
	}

	var oldList []model.SourceCategory
	if err := db.Mdb.Where("source_id = ?", sourceId).Order("source_type_id ASC").Find(&oldList).Error; err != nil {
		return nil, err
	}

	// 新树一个分类都没有，旧树还有
	if len(oldList) > 0 && len(plans) == 0 {
		return nil, errors.New("采集站返回的分类为空，未更新")
	}

	// 该站还没有分类，直接保存无差异
	if len(oldList) == 0 {
		return nil, nil
	}

	return DiffSourceCategoryTree(oldList, plans), nil
}

// clearChangedSourceDataTx 在事务中清空该站采集数据，并摘除变动分类在存活影片上的关联
func clearChangedSourceDataTx(tx *gorm.DB, sourceId string, changes []model.CategoryTreeChange) (CategoryApplyResult, error) {
	var hasPlaylist int64
	if err := tx.Model(&model.FilmSourcePlaylist{}).Where("source_id = ?", sourceId).Count(&hasPlaylist).Error; err != nil {
		return CategoryApplyResult{}, err
	}
	var hasPoster int64
	if err := tx.Model(&model.MoviePoster{}).Where("source_id = ?", sourceId).Count(&hasPoster).Error; err != nil {
		return CategoryApplyResult{}, err
	}
	var hasMapping int64
	if err := tx.Model(&model.MovieSourceMapping{}).Where("source_id = ?", sourceId).Count(&hasMapping).Error; err != nil {
		return CategoryApplyResult{}, err
	}
	var hasFailures int64
	if err := tx.Model(&model.FailureRecord{}).Where("origin_id = ?", sourceId).Count(&hasFailures).Error; err != nil {
		return CategoryApplyResult{}, err
	}

	orphanMids, err := CascadeCleanSourceDataTx(tx, sourceId)
	if err != nil {
		return CategoryApplyResult{}, err
	}

	if err := DeleteFailureRecordsByOriginIdTx(tx, sourceId); err != nil {
		return CategoryApplyResult{}, err
	}

	dataCleared := hasPlaylist > 0 || hasPoster > 0 || hasMapping > 0 || hasFailures > 0 || len(orphanMids) > 0

	changedTypeKeys := make([]string, 0, len(changes))
	for _, c := range changes {
		changedTypeKeys = append(changedTypeKeys, fmt.Sprintf("source:%s:%d", sourceId, c.TypeId))
	}

	var detachedMids []int64
	if len(changedTypeKeys) > 0 {
		dq := tx.Model(&model.FilmIndex{}).Where("category_key IN (?) OR root_category_key IN (?)", changedTypeKeys, changedTypeKeys)
		if len(orphanMids) > 0 {
			dq = dq.Where("mid NOT IN (?)", orphanMids)
		}
		if err := dq.Pluck("mid", &detachedMids).Error; err != nil {
			return CategoryApplyResult{}, err
		}
		if len(detachedMids) > 0 {
			clearFields := map[string]any{
				"category_key":      "",
				"root_category_key": "",
				"cid":               0,
				"pid":               0,
				"c_name":            "",
				"original_category": "",
			}
			if err := tx.Model(&model.FilmIndex{}).Where("mid IN (?)", detachedMids).Updates(clearFields).Error; err != nil {
				return CategoryApplyResult{}, err
			}
			dataCleared = true
		}
	}

	return CategoryApplyResult{
		DataCleared:  dataCleared,
		OrphanMids:   orphanMids,
		DetachedMids: detachedMids,
	}, nil
}
