package repository

import (
	"fmt"
	"strings"
	"sync"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

var otherCategoryMu sync.Mutex

// EnsureOtherCategory 保证该站有一级「其他/其它」，供未映射影片归入。
func EnsureOtherCategory(sourceId string) (typeID int64, categoryID int64, err error) {
	sourceId = strings.TrimSpace(sourceId)
	if sourceId == "" || sourceId == "manual" {
		return 0, 0, fmt.Errorf("source id 不能为空")
	}
	if typeID, categoryID, ok := support.LookupOtherSourceCategory(sourceId); ok {
		return typeID, categoryID, nil
	}
	otherCategoryMu.Lock()
	defer otherCategoryMu.Unlock()
	if typeID, categoryID, ok := support.LookupOtherSourceCategory(sourceId); ok {
		return typeID, categoryID, nil
	}
	typeID, categoryID, err = createOtherCategory(sourceId)
	if err != nil {
		return 0, 0, err
	}
	MarkCategoryChanged()
	return typeID, categoryID, nil
}

func appendOtherCategoryPlan(plans []sourceCategoryPlacement, old []model.SourceCategory) []sourceCategoryPlacement {
	if len(plans) == 0 {
		return plans
	}
	used := make(map[int64]struct{}, len(plans))
	maxRootSort := 0
	for _, plan := range plans {
		used[plan.SourceTypeId] = struct{}{}
		if plan.ParentSourceTypeId == 0 && model.IsOtherCategoryName(plan.Name) {
			return plans
		}
		if plan.ParentSourceTypeId == 0 && plan.Sort > maxRootSort {
			maxRootSort = plan.Sort
		}
	}
	for _, row := range old {
		if row.ParentSourceTypeId != 0 || !model.IsOtherCategoryName(row.RawName) {
			continue
		}
		if _, taken := used[row.SourceTypeId]; taken {
			continue
		}
		return append(plans, sourceCategoryPlacement{
			SourceTypeId:       row.SourceTypeId,
			ParentSourceTypeId: 0,
			Name:               strings.TrimSpace(row.RawName),
			Sort:               row.Sort,
			Depth:              row.Depth,
		})
	}
	sort := model.OtherCategorySort
	if maxRootSort >= sort {
		sort = maxRootSort + 1
	}
	return append(plans, sourceCategoryPlacement{
		SourceTypeId:       nextOtherSourceTypeID(used),
		ParentSourceTypeId: 0,
		Name:               model.TagOthersName,
		Sort:               sort,
		Depth:              0,
	})
}

func nextOtherSourceTypeID(used map[int64]struct{}) int64 {
	id := model.OtherSourceTypeID
	for {
		if _, ok := used[id]; !ok {
			return id
		}
		id++
	}
}

func createOtherCategory(sourceId string) (int64, int64, error) {
	var typeID, categoryID int64
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		var rows []model.SourceCategory
		if err := tx.Where("source_id = ?", sourceId).Order("source_type_id ASC").Find(&rows).Error; err != nil {
			return err
		}
		used := make(map[int64]struct{}, len(rows))
		maxRootSort := 0
		var existing *model.SourceCategory
		for i := range rows {
			row := &rows[i]
			used[row.SourceTypeId] = struct{}{}
			if row.ParentSourceTypeId == 0 && row.Sort > maxRootSort {
				maxRootSort = row.Sort
			}
			if existing == nil && row.ParentSourceTypeId == 0 && model.IsOtherCategoryName(row.RawName) {
				existing = row
			}
		}
		if existing != nil {
			typeID = existing.SourceTypeId
			categoryID = existingMappedCategoryID(tx, sourceId, typeID)
			if categoryID > 0 {
				return nil
			}
			id, err := insertOtherDisplayCategory(tx, sourceId, typeID, existing.Sort)
			if err != nil {
				return err
			}
			categoryID = id
			return insertOtherMapping(tx, sourceId, typeID, categoryID)
		}
		typeID = nextOtherSourceTypeID(used)
		sort := model.OtherCategorySort
		if maxRootSort >= sort {
			sort = maxRootSort + 1
		}
		id, err := insertOtherDisplayCategory(tx, sourceId, typeID, sort)
		if err != nil {
			return err
		}
		categoryID = id
		if err := tx.Create(&model.SourceCategory{
			SourceId:           sourceId,
			SourceTypeId:       typeID,
			ParentSourceTypeId: 0,
			RawName:            model.TagOthersName,
			Sort:               sort,
			Depth:              0,
		}).Error; err != nil {
			return err
		}
		return insertOtherMapping(tx, sourceId, typeID, categoryID)
	})
	return typeID, categoryID, err
}

func existingMappedCategoryID(tx *gorm.DB, sourceId string, typeID int64) int64 {
	var mapping model.CategoryMapping
	if err := tx.Where("source_id = ? AND source_type_id = ?", sourceId, typeID).First(&mapping).Error; err != nil {
		return 0
	}
	if mapping.CategoryId <= 0 {
		return 0
	}
	var category model.Category
	if err := tx.Where("id = ?", mapping.CategoryId).First(&category).Error; err != nil {
		return 0
	}
	return category.Id
}

func insertOtherDisplayCategory(tx *gorm.DB, sourceId string, typeID int64, sort int) (int64, error) {
	stableKey := support.BuildSourceCategoryKey(sourceId, typeID)
	var existing model.Category
	if err := tx.Where("stable_key = ?", stableKey).First(&existing).Error; err == nil && existing.Id > 0 {
		return existing.Id, nil
	}
	category := model.Category{
		Pid:       0,
		Name:      model.TagOthersName,
		StableKey: stableKey,
		Show:      true,
		Sort:      sort,
	}
	if err := tx.Create(&category).Error; err != nil {
		if tx.Where("stable_key = ?", stableKey).First(&existing).Error == nil && existing.Id > 0 {
			return existing.Id, nil
		}
		return 0, err
	}
	return category.Id, nil
}

func insertOtherMapping(tx *gorm.DB, sourceId string, typeID, categoryID int64) error {
	var mapping model.CategoryMapping
	if err := tx.Where("source_id = ? AND source_type_id = ?", sourceId, typeID).First(&mapping).Error; err == nil {
		if mapping.CategoryId == categoryID {
			return nil
		}
		return tx.Model(&mapping).Update("category_id", categoryID).Error
	}
	return tx.Create(&model.CategoryMapping{
		SourceId:     sourceId,
		SourceTypeId: typeID,
		CategoryId:   categoryID,
	}).Error
}
