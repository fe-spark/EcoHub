package repository

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"
)

func GetSourceCategoryTree(sourceId string) (*model.CategoryTree, error) {
	sourceId = strings.TrimSpace(sourceId)
	if sourceId == "" {
		return nil, fmt.Errorf("source id 不能为空")
	}
	var rows []model.SourceCategory
	if err := db.Mdb.Where("source_id = ?", sourceId).Order("depth ASC, parent_source_type_id ASC, sort ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return buildCategoryTreeFromSourceRows(rows)
}

func buildCategoryTreeFromSourceRows(rows []model.SourceCategory) (*model.CategoryTree, error) {
	root := &model.CategoryTree{Id: 0, Pid: -1, Name: "分类信息", Show: true, Children: make([]*model.CategoryTree, 0)}
	nodes := make(map[int64]*model.CategoryTree, len(rows))
	for _, row := range rows {
		name := strings.TrimSpace(row.RawName)
		if name == "" {
			return nil, fmt.Errorf("来源分类名称不能为空: %d", row.SourceTypeId)
		}
		nodes[row.SourceTypeId] = &model.CategoryTree{
			Id:       row.SourceTypeId,
			Pid:      row.ParentSourceTypeId,
			Name:     name,
			Sort:     row.Sort,
			Show:     true,
			Children: make([]*model.CategoryTree, 0),
		}
	}
	for _, row := range rows {
		node, ok := nodes[row.SourceTypeId]
		if !ok {
			return nil, fmt.Errorf("来源分类节点不存在: %d", row.SourceTypeId)
		}
		if row.ParentSourceTypeId == 0 {
			root.Children = append(root.Children, node)
			continue
		}
		parent, ok := nodes[row.ParentSourceTypeId]
		if !ok {
			return nil, fmt.Errorf("来源父分类不存在: %d", row.ParentSourceTypeId)
		}
		parent.Children = append(parent.Children, node)
	}
	sortCategoryTreeNodes(root.Children)
	return root, nil
}

func sortCategoryTreeNodes(nodes []*model.CategoryTree) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].Sort == nodes[j].Sort {
			return nodes[i].Id < nodes[j].Id
		}
		return nodes[i].Sort < nodes[j].Sort
	})
	for _, node := range nodes {
		if len(node.Children) > 0 {
			sortCategoryTreeNodes(node.Children)
		}
	}
}

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

func GetCategoryTreeByID(id int64) *model.CategoryTree {
	if id <= 0 {
		return nil
	}

	var current model.Category
	if err := db.Mdb.Where("id = ?", id).First(&current).Error; err != nil {
		return nil
	}

	node := &model.CategoryTree{
		Id:        current.Id,
		Pid:       current.Pid,
		Name:      current.Name,
		StableKey: current.StableKey,
		Alias:     current.Alias,
		Show:      current.Show,
		Sort:      current.Sort,
		CreatedAt: current.CreatedAt,
		UpdatedAt: current.UpdatedAt,
		Children:  make([]*model.CategoryTree, 0),
	}

	if current.Pid != 0 {
		return node
	}

	var children []model.Category
	if err := db.Mdb.Where("pid = ?", current.Id).Order("sort ASC, id ASC").Find(&children).Error; err != nil {
		return nil
	}
	for _, child := range children {
		item := child
		node.Children = append(node.Children, &model.CategoryTree{
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
		})
	}

	return node
}

// GetActiveCategoryTree 获取前台导航分类树。优先 Redis；未命中时用物化 ID 或分类映射构建。
func GetActiveCategoryTree() model.CategoryTree {
	// 1. 尝试从 Redis 获取
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, config.ActiveCategoryTreeKey).Result(); err == nil && data != "" {
			var tree model.CategoryTree
			if json.Unmarshal([]byte(data), &tree) == nil && isValidActiveCategoryTree(tree) {
				return tree
			}
			log.Printf("[Category] 活跃分类树缓存失效或非法，清除缓存并重新构建")
			db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)
		}
	}

	activeCategoryMap := loadActiveCategoryIDsFromCurrentMappings()
	activeVisibleMap := buildActiveCategoryAncestorMap(activeCategoryMap)

	// 3. 构建树
	var allList []model.Category
	db.Mdb.Where("`show` = ?", true).Order("pid ASC, sort ASC, id ASC").Find(&allList)

	nodes := make(map[int64]*model.CategoryTree)
	root := model.CategoryTree{
		Id: 0, Pid: -1, Name: "分类信息", Show: true,
		Children: make([]*model.CategoryTree, 0),
	}

	// 第一遍：创建所有节点
	for _, c := range allList {
		node := &model.CategoryTree{
			Id:        c.Id,
			Pid:       c.Pid,
			Name:      c.Name,
			StableKey: c.StableKey,
			Alias:     c.Alias,
			Show:      c.Show,
			Sort:      c.Sort,
			CreatedAt: c.CreatedAt,
			UpdatedAt: c.UpdatedAt,
			Children:  make([]*model.CategoryTree, 0),
		}
		nodes[c.Id] = node
	}

	// 第二遍：按活跃分类及其祖先关系挂载整棵展示树。
	for _, c := range allList {
		if !activeVisibleMap[c.Id] || c.Pid == 0 {
			continue
		}
		if parent, ok := nodes[c.Pid]; ok && activeVisibleMap[c.Pid] {
			parent.Children = append(parent.Children, nodes[c.Id])
		}
	}

	// 第三遍：收集活跃的大类到根节点下
	for _, c := range allList {
		if c.Pid != 0 {
			continue
		}
		node := nodes[c.Id]
		if activeVisibleMap[c.Id] {
			root.Children = append(root.Children, node)
		}
	}
	sortRootCategories(root.Children)

	// 7. 写入 Redis 缓存 (1小时)
	if db.Rdb != nil {
		if data, err := json.Marshal(root); err == nil {
			db.Rdb.Set(db.Cxt, config.ActiveCategoryTreeKey, string(data), time.Hour)
		}
	}

	return root
}

func loadActiveCategoryIDsFromCurrentMappings() map[int64]bool {
	active := make(map[int64]bool)

	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, config.ActiveCategoryIDsKey).Result(); err == nil && data != "" {
			var ids []int64
			if json.Unmarshal([]byte(data), &ids) == nil && len(ids) > 0 {
				for _, id := range ids {
					if id > 0 {
						active[id] = true
					}
				}
				if len(active) > 0 {
					return active
				}
			}
		}
	}

	if db.Mdb != nil {
		var categoryIDs []int64
		if err := db.Mdb.Model(&model.CategoryMapping{}).
			Where("category_id > 0").
			Pluck("category_id", &categoryIDs).Error; err == nil {
			for _, id := range categoryIDs {
				if id > 0 {
					active[id] = true
				}
			}
		}
	}

	return active
}

func buildActiveCategoryAncestorMap(activeCategoryMap map[int64]bool) map[int64]bool {
	visible := make(map[int64]bool, len(activeCategoryMap))
	for categoryID := range activeCategoryMap {
		currentID := categoryID
		for currentID > 0 {
			if visible[currentID] {
				break
			}
			visible[currentID] = true
			currentID = support.GetParentId(currentID)
		}
	}
	return visible
}

func isValidActiveCategoryTree(tree model.CategoryTree) bool {
	for _, child := range tree.Children {
		if child == nil || child.Pid != 0 || !IsRootCategory(child.Id) {
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

// GetChildrenTree 获取对应主分类下的子分类列表 (实时查库)
func GetChildrenTree(pid int64) []*model.CategoryTree {
	tree := buildTreeHelper()

	if pid == 0 {
		return tree.Children
	}
	for _, c := range tree.Children {
		if c.Id == pid {
			return c.Children
		}
	}
	return nil
}
