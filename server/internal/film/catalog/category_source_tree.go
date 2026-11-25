package catalog

import (
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repobase"
)

// GetSourceBoundCategoryTree 分类管理树。分组、排序、显示与前台同一套规则。
// 节点 ID 仍是 film_category.id，供保存；规则新合成的父组拥有独立合成行，不再借用首个子项 ID。
func GetSourceBoundCategoryTree(sourceID string) model.CategoryTree {
	return GetSourceBoundCategoryTreeWithCounts(sourceID, nil)
}

// GetSourceBoundCategoryTreeWithCounts 构建分类管理树，并按传入的 category_key 计数映射填充各节点影视数量。
func GetSourceBoundCategoryTreeWithCounts(sourceID string, keyCounts map[string]int64) model.CategoryTree {
	root := model.CategoryTree{
		Id: 0, Pid: -1, Name: "分类信息", Show: true,
		Children: make([]*model.CategoryTree, 0),
	}
	sourceID = strings.TrimSpace(sourceID)
	if db.Mdb == nil || sourceID == "" {
		return root
	}
	view, err := loadPublicSourceTypes(sourceID)
	if err != nil || len(view.Groups) == 0 {
		return root
	}
	root.Children = adminTreeFromGroups(view.Groups, 0, sourceID, keyCounts, loadSourceTypeCategories(sourceID), loadSyntheticRootCategories(sourceID))
	return root
}

// SourceCategoryFilmCountsFromDB 从 film_index 按分类树实际使用的 category_key 精确集合聚合统计指定采集站各分类影片数。
// 使用 IN (精确 key) 替代字符区间 [prefix:, prefix;) 比较，避免 MySQL utf8mb4_unicode_ci 下 ';' 排在 ':' 之前导致区间恒空。
func SourceCategoryFilmCountsFromDB(sourceID string) (map[string]int64, error) {
	sourceID = strings.TrimSpace(sourceID)
	out := make(map[string]int64)
	if db.Mdb == nil || sourceID == "" {
		return out, nil
	}
	view, err := loadPublicSourceTypes(sourceID)
	if err != nil || len(view.Groups) == 0 {
		return out, err
	}
	keys := collectSourceTreeCategoryKeys(sourceID, view.Groups)
	if len(keys) == 0 {
		return out, nil
	}
	const batchSize = 500
	for start := 0; start < len(keys); start += batchSize {
		end := start + batchSize
		if end > len(keys) {
			end = len(keys)
		}
		var rows []struct {
			CategoryKey string `gorm:"column:category_key"`
			Total       int64  `gorm:"column:total"`
		}
		if err := db.Mdb.Table(model.TableFilmIndex).
			Select("category_key, COUNT(*) AS total").
			Where("deleted_at IS NULL AND category_key IN ?", keys[start:end]).
			Group("category_key").
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			if key := strings.TrimSpace(r.CategoryKey); key != "" {
				out[key] += r.Total
			}
		}
	}
	return out, nil
}

// collectSourceTreeCategoryKeys 汇总分类树所有分组关联的去重 category_key 列表。
func collectSourceTreeCategoryKeys(sourceID string, groups []publicTypeGroup) []string {
	seen := make(map[string]struct{}, len(groups)*4)
	keys := make([]string, 0, len(groups)*4)
	for _, group := range groups {
		for _, typeID := range collectGroupAllTypeIDs(group) {
			key := repobase.BuildSourceCategoryKey(sourceID, typeID)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	return keys
}

func loadSyntheticRootCategories(sourceID string) map[int64]model.Category {
	out := make(map[int64]model.Category)
	if db.Mdb == nil || strings.TrimSpace(sourceID) == "" {
		return out
	}
	var rows []model.Category
	if err := db.Mdb.Where("stable_key LIKE ?", syntheticRootPrefix(sourceID)+"%").Find(&rows).Error; err != nil {
		return out
	}
	for _, item := range rows {
		if item.Id > 0 {
			out[item.Id] = item
		}
	}
	return out
}

func loadSourceTypeCategories(sourceID string) map[int64]model.Category {
	out := make(map[int64]model.Category)
	var mappings []model.CategoryMapping
	if err := db.Mdb.Where("source_id = ? AND category_id > 0", sourceID).Find(&mappings).Error; err != nil {
		return out
	}
	ids := make([]int64, 0, len(mappings))
	seen := make(map[int64]struct{}, len(mappings))
	for _, mapping := range mappings {
		if _, ok := seen[mapping.CategoryId]; ok {
			continue
		}
		seen[mapping.CategoryId] = struct{}{}
		ids = append(ids, mapping.CategoryId)
	}
	if len(ids) == 0 {
		return out
	}
	var categories []model.Category
	if err := db.Mdb.Where("id IN ?", ids).Find(&categories).Error; err != nil {
		return out
	}
	byID := make(map[int64]model.Category, len(categories))
	for _, item := range categories {
		byID[item.Id] = item
	}
	for _, mapping := range mappings {
		if item, ok := byID[mapping.CategoryId]; ok {
			out[mapping.SourceTypeId] = item
		}
	}
	return out
}

func collectGroupAllTypeIDs(group publicTypeGroup) []int64 {
	seen := make(map[int64]struct{}, len(group.Members)+len(group.Children)*2)
	out := make([]int64, 0, len(group.Members)+len(group.Children)*2)
	var walk func(g publicTypeGroup)
	walk = func(g publicTypeGroup) {
		for _, id := range g.Members {
			if id <= 0 {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
		for _, child := range g.Children {
			walk(child)
		}
	}
	walk(group)
	return out
}

func countGroupFilms(sourceID string, group publicTypeGroup, keyCounts map[string]int64) int64 {
	if len(keyCounts) == 0 || sourceID == "" {
		return 0
	}
	var total int64
	for _, typeID := range collectGroupAllTypeIDs(group) {
		if key := repobase.BuildSourceCategoryKey(sourceID, typeID); key != "" {
			total += keyCounts[key]
		}
	}
	return total
}

func adminTreeFromGroups(groups []publicTypeGroup, parentID int64, sourceID string, keyCounts map[string]int64, typeToCat map[int64]model.Category, syntheticCats map[int64]model.Category) []*model.CategoryTree {
	out := make([]*model.CategoryTree, 0, len(groups))
	for _, group := range groups {
		node, memberIDs, ok := adminNodeFromGroup(group, parentID, sourceID, keyCounts, typeToCat, syntheticCats)
		if !ok {
			continue
		}
		node.Children = adminTreeFromGroups(group.Children, node.Id, sourceID, keyCounts, typeToCat, syntheticCats)
		node.MemberIds = leftoverMemberIDs(node, memberIDs)
		out = append(out, node)
	}
	return out
}

func adminNodeFromGroup(group publicTypeGroup, parentID int64, sourceID string, keyCounts map[string]int64, typeToCat map[int64]model.Category, syntheticCats map[int64]model.Category) (*model.CategoryTree, []int64, bool) {
	var (
		item      model.Category
		memberIDs []int64
		ok        bool
	)
	if group.Synthetic {
		item, ok = syntheticCats[group.CategoryID]
	} else {
		item, memberIDs, ok = mappedAdminCategory(group, typeToCat)
	}
	if !ok {
		return nil, nil, false
	}
	return &model.CategoryTree{
		Id:        item.Id,
		Pid:       parentID,
		Name:      group.Name,
		StableKey: item.StableKey,
		Alias:     item.Alias,
		Show:      group.Show,
		Sort:      group.Sort,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
		Children:  make([]*model.CategoryTree, 0),
		FilmCount: countGroupFilms(sourceID, group, keyCounts),
	}, memberIDs, true
}

func mappedAdminCategory(group publicTypeGroup, typeToCat map[int64]model.Category) (model.Category, []int64, bool) {
	memberIDs := make([]int64, 0, len(group.Members))
	seen := make(map[int64]struct{}, len(group.Members))
	var item model.Category
	found := false
	if mapped, ok := typeToCat[group.ID]; ok && mapped.Id > 0 {
		item = mapped
		found = true
	}
	for _, typeID := range group.Members {
		mapped, ok := typeToCat[typeID]
		if !ok || mapped.Id <= 0 {
			continue
		}
		if _, dup := seen[mapped.Id]; dup {
			continue
		}
		seen[mapped.Id] = struct{}{}
		memberIDs = append(memberIDs, mapped.Id)
		if !found {
			item = mapped
			found = true
		}
	}
	if !found {
		return model.Category{}, nil, false
	}
	return item, memberIDs, true
}

func leftoverMemberIDs(node *model.CategoryTree, memberIDs []int64) []int64 {
	used := map[int64]struct{}{node.Id: {}}
	for _, child := range node.Children {
		used[child.Id] = struct{}{}
		for _, id := range child.MemberIds {
			if id > 0 {
				used[id] = struct{}{}
			}
		}
	}
	extra := make([]int64, 0)
	for _, id := range memberIDs {
		if _, ok := used[id]; ok {
			continue
		}
		extra = append(extra, id)
	}
	if len(extra) == 0 {
		return nil
	}
	return append([]int64{node.Id}, extra...)
}
