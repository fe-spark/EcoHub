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

// HasLiveCategoryKeys 当前展示分类是否已有来源映射。没有映射时列表仍按 pid/cid 过滤。
func HasLiveCategoryKeys(field string, categoryID int64) bool {
	_, _, visible, roots := liveCategoryKeySets(field, categoryID)
	return len(visible) > 0 || len(roots) > 0
}

// LiveCategoryMatchSQL 展示分类的匹配条件。
// 新采集按该站已保存的映射写入 pid/cid。更早入库、pid 仍为 0 的片子靠 category_key 对上展示分类。
func LiveCategoryMatchSQL(alias, field string, categoryID int64) (string, []any) {
	col, id, visible, roots := liveCategoryKeySets(field, categoryID)
	if alias != "" {
		col = alias + "." + col
	}
	keyCol := "category_key"
	rootCol := "root_category_key"
	if alias != "" {
		keyCol = alias + "." + keyCol
		rootCol = alias + "." + rootCol
	}
	if len(visible) == 0 && len(roots) == 0 {
		return col + " = ?", []any{id}
	}
	parts := []string{col + " = ?"}
	args := []any{id}
	if len(visible) > 0 {
		parts = append(parts, keyCol+" IN ("+sqlPlaceholders(len(visible))+")")
		for _, key := range visible {
			args = append(args, key)
		}
	}
	if field != "cid" && len(roots) > 0 {
		parts = append(parts, rootCol+" IN ("+sqlPlaceholders(len(roots))+")")
		for _, key := range roots {
			args = append(args, key)
		}
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

// ApplyLiveCategoryMatch 把展示分类条件加到 film_index 查询上。
func ApplyLiveCategoryMatch(query *gorm.DB, field string, categoryID int64) *gorm.DB {
	if query == nil {
		return nil
	}
	cond, args := LiveCategoryMatchSQL("", field, categoryID)
	return query.Where(cond, args...)
}

// LiveCategoryMemberSQL 从指定采集站的播放线路取出属于该展示分类的 mid。
func LiveCategoryMemberSQL(dialectName, sourceID, field string, categoryID int64) (string, []any) {
	hint := ""
	if dialectName == "mysql" {
		hint = " USE INDEX (`idx_playlist_source_mid`)"
	}
	cond, condArgs := LiveCategoryMatchSQL("i", field, categoryID)
	memberSQL := "SELECT DISTINCT p.mid FROM " + model.TableFilmSourcePlaylist + " AS p" + hint +
		" INNER JOIN " + model.TableFilmIndex + " AS i ON i.mid = p.mid AND i.deleted_at IS NULL" +
		" WHERE p.source_id = ? AND p.line_kind = ? AND " + cond
	args := make([]any, 0, 2+len(condArgs))
	args = append(args, strings.TrimSpace(sourceID), "play")
	args = append(args, condArgs...)
	return memberSQL, args
}

func liveCategoryKeySets(field string, categoryID int64) (col string, id int64, visible []string, roots []string) {
	col = "pid"
	if field == "cid" {
		col = "cid"
	}
	if db.Mdb == nil || categoryID <= 0 {
		return col, categoryID, nil, nil
	}
	id = support.ResolveCategoryID(categoryID)
	if field == "cid" {
		return "cid", id, mappingCategoryKeys(visibleCategoryAndDescendantIDs(id)), nil
	}
	rootID := id
	if category, ok := categoryByID(id); ok && category.Pid > 0 {
		if resolved := support.GetRootId(id); resolved > 0 {
			rootID = resolved
		}
	}
	return "pid", id, mappingCategoryKeys(visibleCategoryAndDescendantIDs(rootID)), mappingCategoryKeys([]int64{rootID})
}

func mappingCategoryKeys(categoryIDs []int64) []string {
	if db.Mdb == nil || len(categoryIDs) == 0 {
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

func sqlPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}

// ApplyManageCategoryFieldFilter 专供管理后台多维检索分类过滤（不校验前台 show 状态，兼容聚合映射与物理 pid/cid）
func ApplyManageCategoryFieldFilter(query *gorm.DB, field string, id int64) *gorm.DB {
	resolvedID := support.ResolveCategoryID(id)
	if resolvedID <= 0 {
		return emptyFilmIndexQuery(query)
	}
	if keys := categorySourceKeys(field, resolvedID); len(keys) > 0 {
		if field == "pid" {
			rootKeys, visibleKeys := rootCategorySourceKeyGroups(resolvedID)
			cond := db.Mdb.Where("category_key IN ? OR pid = ?", visibleKeys, resolvedID)
			if len(rootKeys) > 0 {
				cond = cond.Or("root_category_key IN ? AND (category_key = '' OR category_key IS NULL)", rootKeys)
			}
			return query.Where(cond)
		}
		return query.Where("(category_key IN ? OR cid = ?)", keys, resolvedID)
	}
	if stableKey := categoryStableKey(resolvedID); stableKey != "" {
		return query.Where(fmt.Sprintf("(%s = ? OR %s = ?)", categoryKeyColumn(field), categoryIDColumn(field)), stableKey, resolvedID)
	}
	return query.Where(fmt.Sprintf("%s = ?", categoryIDColumn(field)), resolvedID)
}
