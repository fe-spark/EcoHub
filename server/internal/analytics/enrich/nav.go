package enrich

import (
	"fmt"
	"strings"

	"server/internal/film/catalog"
	"server/internal/model"
)

// CategoryNavTarget 当前主站激活分类树中的有效前端跳转目标
type CategoryNavTarget struct {
	ID   int64
	Pid  int64
	Name string
	Href string
}

// CategoryNavIndex 当前主站可见分类导航索引（按 ID 与名称快速定位）
type CategoryNavIndex struct {
	byID   map[int64]CategoryNavTarget
	byName map[string]CategoryNavTarget
}

var loadActiveCategoryTree = func() (model.CategoryTree, error) {
	return catalog.GetActiveCategoryTree()
}

// LoadCategoryNavIndex 加载当前主站激活分类树的导航索引
func LoadCategoryNavIndex() *CategoryNavIndex {
	tree, err := loadActiveCategoryTree()
	if err != nil {
		return &CategoryNavIndex{}
	}
	return BuildCategoryNavIndex(tree)
}

// BuildCategoryNavIndex 从主站可见分类树构建跳转索引
// 一级可见大类 -> /category?pid=<id>
// 二级可见子类 -> /category/filter?pid=<parentId>&category=<id>
func BuildCategoryNavIndex(tree model.CategoryTree) *CategoryNavIndex {
	idx := &CategoryNavIndex{
		byID:   make(map[int64]CategoryNavTarget),
		byName: make(map[string]CategoryNavTarget),
	}

	register := func(target CategoryNavTarget, memberIDs []int64, overwrite bool) {
		if target.ID <= 0 || target.Href == "" {
			return
		}
		if overwrite {
			idx.byID[target.ID] = target
		} else if _, exists := idx.byID[target.ID]; !exists {
			idx.byID[target.ID] = target
		}
		for _, mid := range memberIDs {
			if mid <= 0 {
				continue
			}
			if overwrite {
				idx.byID[mid] = target
			} else if _, exists := idx.byID[mid]; !exists {
				idx.byID[mid] = target
			}
		}
		name := strings.TrimSpace(target.Name)
		if name != "" {
			if overwrite {
				idx.byName[name] = target
			} else if _, exists := idx.byName[name]; !exists {
				idx.byName[name] = target
			}
		}
	}

	// 先注册一级可见大类（同名时优先一级大类）
	for _, root := range tree.Children {
		if root == nil || !root.Show || root.Id <= 0 {
			continue
		}
		rootTarget := CategoryNavTarget{
			ID:   root.Id,
			Pid:  0,
			Name: strings.TrimSpace(root.Name),
			Href: fmt.Sprintf("/category?pid=%d", root.Id),
		}
		register(rootTarget, root.MemberIds, true)
	}

	// 再注册二级可见子分类，跳转至所属一级大类的筛选页并定位该子分类
	for _, root := range tree.Children {
		if root == nil || !root.Show || root.Id <= 0 {
			continue
		}
		for _, sub := range root.Children {
			if sub == nil || !sub.Show || sub.Id <= 0 {
				continue
			}
			subTarget := CategoryNavTarget{
				ID:   sub.Id,
				Pid:  root.Id,
				Name: strings.TrimSpace(sub.Name),
				Href: fmt.Sprintf("/category/filter?pid=%d&category=%d", root.Id, sub.Id),
			}
			register(subTarget, sub.MemberIds, false)
		}
	}

	return idx
}

// LookupByID 按分类 ID（含规则合并的成员 ID）查找主站有效导航目标
func (idx *CategoryNavIndex) LookupByID(id int64) (CategoryNavTarget, bool) {
	if idx == nil || id <= 0 || len(idx.byID) == 0 {
		return CategoryNavTarget{}, false
	}
	target, ok := idx.byID[id]
	return target, ok
}

// LookupByName 按分类名称查找主站有效导航目标
func (idx *CategoryNavIndex) LookupByName(name string) (CategoryNavTarget, bool) {
	if idx == nil || len(idx.byName) == 0 {
		return CategoryNavTarget{}, false
	}
	target, ok := idx.byName[strings.TrimSpace(name)]
	return target, ok
}
