package repository

import (
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
)

// GetSourceBoundCategoryTree 分类管理树。分组、排序、显示与前台同一套规则。
// 节点 ID 仍是 film_category.id，供保存；代表分类不再重复挂成子节点，避免表格 rowKey 冲突。
func GetSourceBoundCategoryTree(sourceID string) model.CategoryTree {
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
	root.Children = adminTreeFromGroups(view.Groups, 0, loadSourceTypeCategories(sourceID), 0)
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
		Show:      group.Show,
		Sort:      group.Sort,
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
