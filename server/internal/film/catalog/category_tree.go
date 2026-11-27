package catalog

import (
	"sort"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/sourcecatalog"
	"server/internal/taxonomy"
)

// buildTreeHelper 内部辅助函数：直接从列表构建树形结构内存模型
func buildTreeHelper() model.CategoryTree {
	root := model.CategoryTree{
		Id: 0, Pid: -1, Name: "分类信息", Show: true,
		Children: make([]*model.CategoryTree, 0),
	}
	if db.Mdb == nil {
		return root
	}
	var allList []model.Category
	db.Mdb.Order("pid ASC, sort ASC, id ASC").Find(&allList)

	nodes := make(map[int64]*model.CategoryTree)

	for _, c := range allList {
		item := c
		node := &model.CategoryTree{
			Id:        item.Id,
			Pid:       item.Pid,
			Name:      item.Name,
			StableKey: item.StableKey,
			Alias:     item.Alias,
			Show:      item.Show,
			Sort:      item.Sort,
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
			Children:  make([]*model.CategoryTree, 0),
		}
		nodes[item.Id] = node

		if item.Pid == 0 {
			root.Children = append(root.Children, node)
		} else if parent, ok := nodes[item.Pid]; ok {
			parent.Children = append(parent.Children, node)
		}
	}
	sortRootCategories(root.Children)

	return root
}

// GetCategoryTree 获取完整分类树副本 (实时查库，不走长期缓存)
func GetCategoryTree() model.CategoryTree {
	return buildTreeHelper()
}

// GetActiveCategoryTree 获取前台导航分类树。支持可选 sourceId 按采集站独立隔离。优先 Redis；未命中时用分类映射构建。
func GetActiveCategoryTree(sourceIdOpt ...string) (model.CategoryTree, error) {
	var sourceId string
	if len(sourceIdOpt) > 0 {
		sourceId = strings.TrimSpace(sourceIdOpt[0])
	}
	if sourceId == "" {
		if activeSrc := sourcecatalog.CurrentPrimarySource(); activeSrc != nil {
			sourceId = activeSrc.Id
		}
	}
	if sourceId != "" {
		return sourceTypeCategoryTree(sourceId)
	}

	return model.CategoryTree{
		Id: 0, Pid: -1, Name: "分类信息", Show: true,
		Children: make([]*model.CategoryTree, 0),
	}, nil
}

func loadActiveCategoryIDs(sourceId string) map[int64]bool {
	active := make(map[int64]bool)

	if db.Mdb != nil {
		var categoryIDs []int64
		query := db.Mdb.Model(&model.CategoryMapping{}).Where("category_id > 0")
		if sourceId != "" {
			query = query.Where("source_id = ?", sourceId)
		}
		if err := query.Pluck("category_id", &categoryIDs).Error; err == nil {
			for _, id := range categoryIDs {
				if id > 0 {
					active[id] = true
				}
			}
		}

	}

	return active
}

func loadActiveCategoryIDsFromCurrentMappings() map[int64]bool {
	return loadActiveCategoryIDs("")
}

func isValidActiveCategoryTree(tree model.CategoryTree) bool {
	for _, child := range tree.Children {
		if child == nil || child.Pid != 0 || !taxonomy.IsRootCategory(child.Id) {
			return false
		}
	}
	return true
}

func sortRootCategories(children []*model.CategoryTree) {
	sort.SliceStable(children, func(i, j int) bool {
		if children[i].Sort != children[j].Sort {
			return children[i].Sort < children[j].Sort
		}
		return children[i].Id < children[j].Id
	})
}

// GetShownRootCategoryIDs 返回当前所有在分类管理中设置显示（Show=true 且 Pid=0）的一级分类 ID
func GetShownRootCategoryIDs() []int64 {
	if db.Mdb == nil {
		return nil
	}
	tree := GetCategoryTree()
	var ids []int64
	for _, c := range tree.Children {
		if c != nil && c.Show && c.Id > 0 {
			ids = append(ids, c.Id)
		}
	}
	return ids
}

// FilterShownCategoryIDs 过滤出当前分类管理中处于显示状态（Show=true）的分类 ID 列表
func FilterShownCategoryIDs(categoryPids []int64) []int64 {
	if len(categoryPids) == 0 {
		return []int64{}
	}
	// 仅在未连接 DB 的离线单测环境下放行，生产连库状态下必须严密根据库内可见性白名单过滤
	if db.Mdb == nil {
		return categoryPids
	}
	shownIDs := GetShownRootCategoryIDs()
	if len(shownIDs) == 0 {
		return []int64{}
	}
	shownMap := make(map[int64]struct{}, len(shownIDs))
	for _, id := range shownIDs {
		shownMap[id] = struct{}{}
	}
	var out []int64
	for _, pid := range categoryPids {
		if _, ok := shownMap[pid]; ok {
			out = append(out, pid)
		}
	}
	return out
}
