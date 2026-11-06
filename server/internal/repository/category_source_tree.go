package repository

import (
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
)

// GetSourceBoundCategoryTree 按该采集站分类规则展示合并树。
// 节点 ID 仍是 film_category.id，方便分类管理改显示和排序。
func GetSourceBoundCategoryTree(sourceID string) model.CategoryTree {
	root := model.CategoryTree{
		Id: 0, Pid: -1, Name: "分类信息", Show: true,
		Children: make([]*model.CategoryTree, 0),
	}
	sourceID = strings.TrimSpace(sourceID)
	if db.Mdb == nil || sourceID == "" {
		return root
	}

	var rows []model.SourceCategory
	if err := db.Mdb.Where("source_id = ?", sourceID).
		Order("depth ASC, sort ASC, source_type_id ASC").
		Find(&rows).Error; err != nil || len(rows) == 0 {
		return root
	}

	parsed := make([]sourceTypeRow, 0, len(rows))
	for _, row := range rows {
		name := strings.TrimSpace(row.RawName)
		if row.SourceTypeId <= 0 || name == "" {
			continue
		}
		parsed = append(parsed, sourceTypeRow{
			ID:     row.SourceTypeId,
			Parent: row.ParentSourceTypeId,
			Name:   name,
			Sort:   row.Sort,
			Show:   true,
		})
	}
	if len(parsed) == 0 {
		return root
	}

	root.Children = adminTreeFromGroups(groupPublicTypes(parsed, sourceID), 0, loadSourceTypeCategories(sourceID), 0)
	sortCategoryTreeNodes(root.Children)
	return root
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

func adminTreeFromGroups(groups []publicTypeGroup, parentID int64, typeToCat map[int64]model.Category, skipType int64) []*model.CategoryTree {
	out := make([]*model.CategoryTree, 0, len(groups))
	for _, group := range groups {
		if skipType > 0 && group.ID == skipType {
			continue
		}
		node, memberIDs, ok := adminNodeFromGroup(group, parentID, typeToCat)
		if !ok {
			continue
		}
		node.Children = adminTreeFromGroups(group.Children, node.Id, typeToCat, group.ID)
		node.MemberIds = leftoverMemberIDs(node, memberIDs)
		out = append(out, node)
	}
	return out
}

func adminNodeFromGroup(group publicTypeGroup, parentID int64, typeToCat map[int64]model.Category) (*model.CategoryTree, []int64, bool) {
	item, memberIDs, ok := mappedAdminCategory(group, typeToCat)
	if !ok {
		return nil, nil, false
	}
	return &model.CategoryTree{
		Id:        item.Id,
		Pid:       parentID,
		Name:      group.Name,
		StableKey: item.StableKey,
		Alias:     item.Alias,
		Show:      item.Show,
		Sort:      item.Sort,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
		Children:  make([]*model.CategoryTree, 0),
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
