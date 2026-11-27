package query

import (
	"strings"

	"gorm.io/gorm"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/taxonomy"
)

func visibleDescendantCategoryIDs(categoryID int64) []int64 {
	if db.Mdb == nil || categoryID <= 0 {
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
	categoryID = taxonomy.ResolveCategoryID(categoryID)
	if categoryID <= 0 {
		return nil
	}
	ids := []int64{categoryID}
	ids = append(ids, visibleDescendantCategoryIDs(categoryID)...)
	return ids
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

func sourceTypeKeys(sourceID string, typeIDs []int64) []string {
	keys := make([]string, 0, len(typeIDs))
	seen := make(map[string]struct{}, len(typeIDs))
	for _, typeID := range typeIDs {
		key := taxonomy.BuildSourceCategoryKey(sourceID, typeID)
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

// sourceTypeMatchCond 当前站这些 type_id 的影片条件。
// 一级分类已在 PublicSourceTypeIDs 中展开为自身及下属子类成员，统一按实际归属的 category_key 匹配。
func sourceTypeMatchCond(columnPrefix, _ string, keys []string) (string, []any) {
	if len(keys) == 0 {
		return "1 = 0", nil
	}
	col := func(name string) string {
		if columnPrefix == "" {
			return name
		}
		return columnPrefix + name
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(keys)), ",")
	args := make([]any, 0, len(keys))
	for _, key := range keys {
		args = append(args, key)
	}
	return col("category_key") + " IN (" + placeholders + ")", args
}

// ApplySourceTypeMatchIDs 匹配该站这一组 type_id。规则合并后的前台分类把成员一起带上。
func ApplySourceTypeMatchIDs(query *gorm.DB, sourceID, field string, typeIDs []int64) *gorm.DB {
	if query == nil {
		return nil
	}
	cond, args := sourceTypeMatchCond("", field, sourceTypeKeys(sourceID, typeIDs))
	return query.Where(cond, args...)
}

// LiveCategoryMemberSQLIDs 从该站播放线路取出这一组 type_id 的 mid。
func LiveCategoryMemberSQLIDs(dialectName, sourceID, field string, typeIDs []int64) (string, []any) {
	cond, condArgs := sourceTypeMatchCond("i.", field, sourceTypeKeys(sourceID, typeIDs))
	memberSQL := "SELECT DISTINCT p.mid FROM " + model.TableFilmSourcePlaylist + " AS p" +
		" INNER JOIN " + model.TableFilmIndex + " AS i ON i.mid = p.mid AND i.deleted_at IS NULL" +
		" WHERE p.source_id = ? AND p.line_kind = ? AND " + cond
	args := make([]any, 0, 2+len(condArgs))
	args = append(args, strings.TrimSpace(sourceID), "play")
	args = append(args, condArgs...)
	return memberSQL, args
}

func categoryByID(id int64) (*model.Category, bool) {
	if db.Mdb == nil {
		return nil, false
	}
	resolvedID := taxonomy.ResolveCategoryID(id)
	if resolvedID <= 0 {
		return nil, false
	}
	var category model.Category
	if err := db.Mdb.Where("id = ?", resolvedID).First(&category).Error; err != nil {
		return nil, false
	}
	return &category, true
}

func liveCategoryKeySets(field string, categoryID int64) (col string, id int64, visible []string, roots []string) {
	col = "pid"
	if field == "cid" {
		col = "cid"
	}
	if db.Mdb == nil || categoryID <= 0 {
		return col, categoryID, nil, nil
	}
	id = taxonomy.ResolveCategoryID(categoryID)
	if field == "cid" {
		return "cid", id, mappingCategoryKeys(visibleCategoryAndDescendantIDs(id)), nil
	}
	rootID := id
	if category, ok := categoryByID(id); ok && category.Pid > 0 {
		if resolved := taxonomy.GetRootId(id); resolved > 0 {
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
		key := taxonomy.BuildSourceCategoryKey(mapping.SourceId, mapping.SourceTypeId)
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
