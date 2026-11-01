package repository

import (
	"server/internal/infra/db"
	"server/internal/model"
)

// GetSourceBoundCategoryTree 只返回该采集站映射到的展示分类，层级仍用展示分类的父子关系。
func GetSourceBoundCategoryTree(sourceID string) model.CategoryTree {
	root := model.CategoryTree{
		Id: 0, Pid: -1, Name: "分类信息", Show: true,
		Children: make([]*model.CategoryTree, 0),
	}
	if db.Mdb == nil || sourceID == "" {
		return root
	}

	var mappings []model.CategoryMapping
	if err := db.Mdb.Where("source_id = ? AND category_id > 0", sourceID).Find(&mappings).Error; err != nil {
		return root
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
		return root
	}

	var categories []model.Category
	if err := db.Mdb.Where("id IN ?", ids).Order("pid ASC, sort ASC, id ASC").Find(&categories).Error; err != nil {
		return root
	}
	nodes := make(map[int64]*model.CategoryTree, len(categories))
	for _, item := range categories {
		category := item
		nodes[category.Id] = &model.CategoryTree{
			Id:        category.Id,
			Pid:       category.Pid,
			Name:      category.Name,
			StableKey: category.StableKey,
			Alias:     category.Alias,
			Show:      category.Show,
			Sort:      category.Sort,
			CreatedAt: category.CreatedAt,
			UpdatedAt: category.UpdatedAt,
			Children:  make([]*model.CategoryTree, 0),
		}
	}
	for _, item := range categories {
		node := nodes[item.Id]
		if item.Pid > 0 {
			if parent, ok := nodes[item.Pid]; ok {
				parent.Children = append(parent.Children, node)
				continue
			}
		}
		root.Children = append(root.Children, node)
	}
	sortRootCategories(root.Children)
	return root
}
