package film

import (
	"fmt"
	"log"
	"slices"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

func hasTextValue(column string) string {
	return fmt.Sprintf("(%s <> '' AND %s IS NOT NULL)", column, column)
}

func isUnknownTextValue(column string) string {
	return fmt.Sprintf("(%s = '' OR %s IS NULL)", column, column)
}

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

func currentCategoryIDsBySourceKey(sourceKey string) []int64 {
	sourceKey = strings.TrimSpace(sourceKey)
	if sourceKey == "" {
		return nil
	}
	var mappings []model.CategoryMapping
	if err := db.Mdb.Find(&mappings).Error; err != nil {
		return nil
	}
	ids := make([]int64, 0)
	seen := make(map[int64]struct{})
	for _, mapping := range mappings {
		if support.BuildSourceCategoryKey(mapping.SourceId, mapping.SourceTypeId) != sourceKey {
			continue
		}
		if mapping.CategoryId <= 0 {
			continue
		}
		if _, ok := seen[mapping.CategoryId]; ok {
			continue
		}
		seen[mapping.CategoryId] = struct{}{}
		ids = append(ids, mapping.CategoryId)
	}
	return ids
}

func currentRootCategoryIDsForSearch(search model.FilmIndex) []int64 {
	ids := currentCategoryIDsBySourceKey(search.RootCategoryKey)
	if len(ids) == 0 && search.Pid > 0 {
		ids = []int64{support.ResolveCategoryID(search.Pid)}
	}
	roots := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		rootID := support.GetRootId(id)
		if rootID <= 0 {
			rootID = id
		}
		if rootID <= 0 {
			continue
		}
		if _, ok := seen[rootID]; ok {
			continue
		}
		seen[rootID] = struct{}{}
		roots = append(roots, rootID)
	}
	return roots
}

func currentCategoryIDsForSearch(search model.FilmIndex) []int64 {
	ids := currentCategoryIDsBySourceKey(search.CategoryKey)
	if len(ids) == 0 && search.Cid > 0 {
		ids = []int64{support.ResolveCategoryID(search.Cid)}
	}
	return ids
}

func applySameCurrentRootCategoryFilter(query *gorm.DB, search model.FilmIndex) *gorm.DB {
	rootIDs := currentRootCategoryIDsForSearch(search)
	if len(rootIDs) == 0 {
		return query.Where("1 = 0")
	}
	rootQuery := db.Mdb.Where("1 = 0")
	for _, rootID := range rootIDs {
		rootQuery = rootQuery.Or(applyCategoryFieldFilter(db.Mdb.Model(&model.FilmIndex{}), "pid", rootID))
	}
	return query.Where(rootQuery)
}

func applySameCurrentCategoryFilter(query *gorm.DB, search model.FilmIndex) *gorm.DB {
	categoryIDs := currentCategoryIDsForSearch(search)
	if len(categoryIDs) == 0 {
		return query.Where("1 = 0")
	}
	categoryQuery := db.Mdb.Where("1 = 0")
	for _, categoryID := range categoryIDs {
		categoryQuery = categoryQuery.Or(applyCategoryFieldFilter(db.Mdb.Model(&model.FilmIndex{}), "cid", categoryID))
	}
	return query.Where(categoryQuery)
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

func visibleCategoryGroups() ([]int64, []int64) {
	var categories []model.Category
	if err := db.Mdb.Where("`show` = ?", true).Find(&categories).Error; err != nil {
		return nil, nil
	}

	rootSet := make(map[int64]struct{}, len(categories))
	for _, category := range categories {
		if category.Pid == 0 {
			rootSet[category.Id] = struct{}{}
		}
	}

	rootIDs := make([]int64, 0, len(rootSet))
	categoryIDs := make([]int64, 0, len(categories))
	for _, category := range categories {
		if category.Pid == 0 {
			rootIDs = append(rootIDs, category.Id)
			categoryIDs = append(categoryIDs, category.Id)
			continue
		}
		if _, ok := rootSet[category.Pid]; ok {
			categoryIDs = append(categoryIDs, category.Id)
		}
	}
	return rootIDs, categoryIDs
}

func stableKeysByCategoryIDs(categoryIDs []int64) []string {
	if len(categoryIDs) == 0 {
		return nil
	}
	var keys []string
	db.Mdb.Model(&model.Category{}).
		Where("id IN ? AND stable_key <> ''", categoryIDs).
		Pluck("stable_key", &keys)
	return slices.Compact(keys)
}

func applyVisibleCategoryFilter(query *gorm.DB) *gorm.DB {
	rootIDs, categoryIDs := visibleCategoryGroups()
	if len(categoryIDs) == 0 {
		return emptyFilmIndexQuery(query)
	}

	rootKeys := sourceCategoryKeysByCategoryIDs(rootIDs)
	visibleKeys := sourceCategoryKeysByCategoryIDs(categoryIDs)
	if len(visibleKeys) == 0 {
		visibleKeys = stableKeysByCategoryIDs(categoryIDs)
		rootKeys = stableKeysByCategoryIDs(rootIDs)
	}
	if len(visibleKeys) > 0 {
		visibleQuery := db.Mdb.Where("category_key IN ?", visibleKeys)
		if len(rootKeys) > 0 {
			visibleQuery = visibleQuery.Or("root_category_key IN ? AND (category_key = '' OR category_key IS NULL)", rootKeys)
		}
		return query.Where(visibleQuery)
	}
	return query.Where("cid IN ? OR (pid IN ? AND cid = 0)", categoryIDs, rootIDs)
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

func applyCategoryFieldFilter(query *gorm.DB, field string, id int64) *gorm.DB {
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

func ApplyCategoryFilter(query *gorm.DB, pid int64, cid int64) *gorm.DB {
	isUncategorized := cid == model.TagUncategorizedValue
	pid = support.ResolveCategoryID(pid)
	if cid > 0 {
		cid = support.ResolveCategoryID(cid)
	}
	switch {
	case isUncategorized && pid > 0:
		if rootKeys, _ := rootCategorySourceKeyGroups(pid); len(rootKeys) > 0 {
			return query.Where("root_category_key IN ? AND (category_key = '' OR category_key IS NULL)", rootKeys)
		}
		if rootKey := categoryStableKey(pid); rootKey != "" {
			return query.Where("root_category_key = ? AND (category_key = '' OR category_key IS NULL)", rootKey)
		}
		return query.Where("pid = ? AND cid = 0", pid)
	case cid > 0 && support.IsRootCategory(cid):
		return applyCategoryFieldFilter(query, "pid", cid)
	case cid > 0:
		return applyCategoryFieldFilter(query, "cid", cid)
	case pid > 0:
		return applyCategoryFieldFilter(query, "pid", pid)
	default:
		return applyVisibleCategoryFilter(query)
	}
}

func applyOriginalCategoryFilter(query *gorm.DB, pid int64, value string) *gorm.DB {
	pid = support.ResolveCategoryID(pid)
	value = strings.TrimSpace(value)
	if pid <= 0 || value == "" {
		return query
	}
	query = applyCategoryFieldFilter(query, "pid", pid)
	return query.Where("original_category = ?", value)
}

func GetOriginalCategoryOptions(pid int64) []string {
	pid = support.ResolveCategoryID(pid)
	if pid <= 0 {
		return nil
	}

	var values []string
	query := applyCategoryFieldFilter(db.Mdb.Model(&model.FilmIndex{}), "pid", pid)
	if err := query.
		Distinct("original_category").
		Where("original_category <> '' AND original_category IS NOT NULL").
		Order("original_category ASC").
		Pluck("original_category", &values).Error; err != nil {
		log.Printf("GetOriginalCategoryOptions Error: %v", err)
		return nil
	}
	values = slices.Compact(values)
	if len(values) <= 1 {
		return nil
	}
	return values
}

func buildCategoryQuery(field string, id int64) *gorm.DB {
	return applyCategoryFieldFilter(db.Mdb.Model(&model.FilmIndex{}), field, id)
}
