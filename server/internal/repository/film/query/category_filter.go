package query

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"
)

func categoryKeyColumn(field string) string {
	if field == "cid" {
		return "category_key"
	}
	return "root_category_key"
}

func categoryIDColumn(field string) string {
	if field == "cid" {
		return "cid"
	}
	return "pid"
}

func categoryStableKey(id int64) string {
	return strings.TrimSpace(support.GetCategoryStableKeyByID(support.ResolveCategoryID(id)))
}

func sourceCategoryKeysByCategoryIDs(categoryIDs []int64) []string {
	if len(categoryIDs) == 0 {
		return nil
	}
	var mappings []model.CategoryMapping
	if err := db.Mdb.Where("category_id IN ?", categoryIDs).Find(&mappings).Error; err != nil {
		return nil
	}
	keys := make([]string, 0, len(mappings))
	seen := make(map[string]struct{}, len(mappings))
	for _, mapping := range mappings {
		key := support.BuildSourceCategoryKey(mapping.SourceId, mapping.SourceTypeId)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

func visibleDescendantCategoryIDs(categoryID int64) []int64 {
	if categoryID <= 0 {
		return nil
	}
	ids := make([]int64, 0)
	queue := []int64{categoryID}
	seen := map[int64]struct{}{categoryID: {}}
	for len(queue) > 0 {
		parentID := queue[0]
		queue = queue[1:]

		var children []model.Category
		db.Mdb.Where("pid = ? AND `show` = ?", parentID, true).Order("sort ASC, id ASC").Find(&children)
		for _, child := range children {
			if child.Id <= 0 {
				continue
			}
			if _, ok := seen[child.Id]; ok {
				continue
			}
			seen[child.Id] = struct{}{}
			ids = append(ids, child.Id)
			queue = append(queue, child.Id)
		}
	}
	return ids
}

func visibleCategoryAndDescendantIDs(categoryID int64) []int64 {
	categoryID = support.ResolveCategoryID(categoryID)
	if categoryID <= 0 {
		return nil
	}
	ids := []int64{categoryID}
	ids = append(ids, visibleDescendantCategoryIDs(categoryID)...)
	return ids
}

func categorySourceKeys(field string, id int64) []string {
	category, ok := categoryByID(id)
	if !ok {
		return nil
	}
	if field == "cid" {
		return sourceCategoryKeysByCategoryIDs(visibleCategoryAndDescendantIDs(category.Id))
	}

	rootID := category.Id
	if category.Pid > 0 {
		rootID = support.GetRootId(category.Id)
	}
	return sourceCategoryKeysByCategoryIDs(visibleCategoryAndDescendantIDs(rootID))
}

func rootCategorySourceKeyGroups(id int64) ([]string, []string) {
	category, ok := categoryByID(id)
	if !ok {
		return nil, nil
	}
	rootID := category.Id
	if category.Pid > 0 {
		rootID = support.GetRootId(category.Id)
	}
	visibleCategoryIDs := visibleCategoryAndDescendantIDs(rootID)
	return sourceCategoryKeysByCategoryIDs([]int64{rootID}), sourceCategoryKeysByCategoryIDs(visibleCategoryIDs)
}

func applyRootCategorySourceFilter(query *gorm.DB, rootID int64) *gorm.DB {
	rootKeys, visibleKeys := rootCategorySourceKeyGroups(rootID)
	if len(visibleKeys) == 0 {
		return query
	}
	visibleCategoryQuery := db.Mdb.Where("category_key IN ?", visibleKeys)
	if len(rootKeys) > 0 {
		visibleCategoryQuery = visibleCategoryQuery.Or("root_category_key IN ? AND (category_key = '' OR category_key IS NULL)", rootKeys)
	}
	return query.Where(visibleCategoryQuery)
}

func categoryByID(id int64) (*model.Category, bool) {
	resolvedID := support.ResolveCategoryID(id)
	if resolvedID <= 0 {
		return nil, false
	}
	var category model.Category
	if err := db.Mdb.Where("id = ?", resolvedID).First(&category).Error; err != nil {
		return nil, false
	}
	return &category, true
}

func emptyFilmIndexQuery(query *gorm.DB) *gorm.DB {
	return query.Where("1 = 0")
}

func applyCategoryVisibilityFilter(query *gorm.DB, field string, id int64) *gorm.DB {
	category, ok := categoryByID(id)
	if !ok || !category.Show {
		return emptyFilmIndexQuery(query)
	}

	if field == "pid" {
		rootID := category.Id
		if category.Pid > 0 {
			rootID = support.GetRootId(category.Id)
		}
		if root, ok := categoryByID(rootID); !ok || !root.Show {
			return emptyFilmIndexQuery(query)
		}
		return query
	}

	if category.Pid > 0 {
		if parent, ok := categoryByID(category.Pid); !ok || !parent.Show {
			return emptyFilmIndexQuery(query)
		}
	}
	return query
}

func ApplyCategoryFieldFilter(query *gorm.DB, field string, id int64) *gorm.DB {
	resolvedID := support.ResolveCategoryID(id)
	if resolvedID <= 0 {
		return emptyFilmIndexQuery(query)
	}
	query = applyCategoryVisibilityFilter(query, field, resolvedID)
	if keys := categorySourceKeys(field, resolvedID); len(keys) > 0 {
		if field == "pid" {
			return applyRootCategorySourceFilter(query, resolvedID)
		}
		return query.Where("category_key IN ?", keys)
	}
	if stableKey := categoryStableKey(resolvedID); stableKey != "" {
		return query.Where(fmt.Sprintf("%s = ?", categoryKeyColumn(field)), stableKey)
	}
	return query.Where(fmt.Sprintf("%s = ?", categoryIDColumn(field)), resolvedID)
}
