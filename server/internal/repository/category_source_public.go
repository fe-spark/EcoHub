package repository

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"
)

// sourceTypeCategoryTree 前台分类树。节点 id 是当前采集站自己的 type_id。
// 首选站套用分类规则：命中的改名，同一父级下目标名相同的 type 合成一个节点。
func sourceTypeCategoryTree(sourceId string) model.CategoryTree {
	root := model.CategoryTree{
		Id: 0, Pid: -1, Name: "分类信息", Show: true,
		Children: make([]*model.CategoryTree, 0),
	}
	sourceId = strings.TrimSpace(sourceId)
	if sourceId == "" || db.Mdb == nil {
		return root
	}

	cacheKey := fmt.Sprintf("%s:src_%s:stvis:r%s:c%s", config.ActiveCategoryTreeKey, sourceId, support.GetRuleVersion(), support.GetCategoryVersion())
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var tree model.CategoryTree
			if json.Unmarshal([]byte(data), &tree) == nil && validSourceTypeTree(tree) {
				return tree
			}
			db.Rdb.Del(db.Cxt, cacheKey)
		}
	}

	view := loadPublicSourceTypes(sourceId)
	for _, group := range view.Groups {
		if !group.Show {
			continue
		}
		root.Children = append(root.Children, group.treeNode(0))
	}
	sortRootCategories(root.Children)

	if db.Rdb != nil {
		if data, err := json.Marshal(root); err == nil {
			db.Rdb.Set(db.Cxt, cacheKey, string(data), time.Hour)
		}
	}
	return root
}

// sourceTypeShown 分类管理里的显示开关。键是该站 type_id。没有映射的分类保持显示。
func sourceTypeShown(sourceId string) map[int64]bool {
	type pair struct {
		SourceTypeId int64 `gorm:"column:source_type_id"`
		Show         bool  `gorm:"column:show"`
	}
	var pairs []pair
	if err := db.Mdb.Model(&model.CategoryMapping{}).
		Select("category_mappings.source_type_id, film_category.show").
		Joins("JOIN film_category ON film_category.id = category_mappings.category_id").
		Where("category_mappings.source_id = ?", sourceId).
		Scan(&pairs).Error; err != nil {
		return map[int64]bool{}
	}
	flags := make(map[int64]bool, len(pairs))
	for _, pair := range pairs {
		if pair.SourceTypeId <= 0 {
			continue
		}
		if prev, ok := flags[pair.SourceTypeId]; ok {
			flags[pair.SourceTypeId] = prev && pair.Show
			continue
		}
		flags[pair.SourceTypeId] = pair.Show
	}
	return flags
}

func validSourceTypeTree(tree model.CategoryTree) bool {
	if strings.TrimSpace(tree.Name) == "" {
		return false
	}
	for _, child := range tree.Children {
		if child == nil || child.Id <= 0 || child.Pid != 0 || strings.TrimSpace(child.Name) == "" {
			return false
		}
	}
	return true
}

// LookupRootSourceType 按当前采集站的一级 type_id 取分类。不是该站的一级分类时返回空。
// 合并后的成员 type 返回合成节点。隐藏的一级分类仍返回，方便直接打开原地址。
func LookupRootSourceType(sourceId string, typeId int64) *model.CategoryTree {
	if typeId <= 0 {
		return nil
	}
	view := loadPublicSourceTypes(sourceId)
	if group, ok := view.groupByMember(typeId); ok && group.Show && group.Parent == 0 {
		node := group.treeNode(0)
		return node
	}
	if row, ok := view.Hidden[typeId]; ok && row.Parent == 0 {
		return &model.CategoryTree{
			Id:       row.ID,
			Pid:      0,
			Name:     row.Name,
			Show:     false,
			Sort:     row.Sort,
			Children: []*model.CategoryTree{},
		}
	}
	return nil
}

// PublicSourceTypeIDs 前台这个分类要包含的本站 type_id。
// 已合并的分类返回全部成员。隐藏分类和未建组的 type 只返回自己。
func PublicSourceTypeIDs(sourceID, field string, typeID int64) []int64 {
	if typeID <= 0 {
		return nil
	}
	view := loadPublicSourceTypes(sourceID)
	if group, ok := view.groupByMember(typeID); ok && group.Show {
		if field == "cid" && group.Parent == 0 {
			return []int64{typeID}
		}
		if field != "cid" && group.Parent != 0 {
			return []int64{typeID}
		}
		out := make([]int64, len(group.Members))
		copy(out, group.Members)
		return out
	}
	return []int64{typeID}
}

// PublicSourceChildNodes 前台一级分类下的子类。已套规则，不含隐藏。
func PublicSourceChildNodes(sourceID string, parentTypeID int64) []*model.CategoryTree {
	view := loadPublicSourceTypes(sourceID)
	group, ok := view.groupByMember(parentTypeID)
	if !ok || !group.Show || group.Parent != 0 {
		return []*model.CategoryTree{}
	}
	nodes := make([]*model.CategoryTree, 0, len(group.Children))
	for i := range group.Children {
		nodes = append(nodes, group.Children[i].treeNode(group.ID))
	}
	return nodes
}

type sourceTypeRow struct {
	ID     int64
	Parent int64
	Name   string
	Sort   int
	Show   bool
}

type publicTypeGroup struct {
	ID       int64
	Parent   int64
	Name     string
	Sort     int
	Show     bool
	Members  []int64
	Children []publicTypeGroup
}

func (g publicTypeGroup) treeNode(parentID int64) *model.CategoryTree {
	node := &model.CategoryTree{
		Id:       g.ID,
		Pid:      parentID,
		Name:     g.Name,
		Show:     true,
		Sort:     g.Sort,
		Children: make([]*model.CategoryTree, 0, len(g.Children)),
	}
	for i := range g.Children {
		node.Children = append(node.Children, g.Children[i].treeNode(g.ID))
	}
	return node
}

type publicSourceView struct {
	Groups  []publicTypeGroup
	members map[int64]publicTypeGroup
	Hidden  map[int64]sourceTypeRow
}

func (v publicSourceView) groupByMember(typeID int64) (publicTypeGroup, bool) {
	group, ok := v.members[typeID]
	if !ok {
		return publicTypeGroup{}, false
	}
	return group, true
}

type publicSourceCacheEntry struct {
	key  string
	view publicSourceView
}

var (
	publicSourceCacheMu sync.Mutex
	publicSourceCache   = map[string]publicSourceCacheEntry{}
)

func loadPublicSourceTypes(sourceID string) publicSourceView {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || db.Mdb == nil {
		return publicSourceView{}
	}
	cacheKey := sourceID + ":" + support.GetRuleVersion() + ":" + support.GetCategoryVersion()
	publicSourceCacheMu.Lock()
	if entry, ok := publicSourceCache[sourceID]; ok && entry.key == cacheKey {
		view := entry.view
		publicSourceCacheMu.Unlock()
		return view
	}
	publicSourceCacheMu.Unlock()

	view := buildPublicSourceView(sourceID)
	publicSourceCacheMu.Lock()
	publicSourceCache[sourceID] = publicSourceCacheEntry{key: cacheKey, view: view}
	publicSourceCacheMu.Unlock()
	return view
}

func buildPublicSourceView(sourceID string) publicSourceView {
	var rows []model.SourceCategory
	if err := db.Mdb.Where("source_id = ?", sourceID).
		Order("depth ASC, sort ASC, source_type_id ASC").
		Find(&rows).Error; err != nil {
		return publicSourceView{}
	}
	shown := sourceTypeShown(sourceID)
	useRules := publicRulesForSource(sourceID)
	parsed := make([]sourceTypeRow, 0, len(rows))
	hidden := map[int64]sourceTypeRow{}
	for _, row := range rows {
		name := strings.TrimSpace(row.RawName)
		if row.SourceTypeId <= 0 || name == "" {
			continue
		}
		item := sourceTypeRow{
			ID:     row.SourceTypeId,
			Parent: row.ParentSourceTypeId,
			Name:   name,
			Sort:   row.Sort,
			Show:   true,
		}
		if flag, ok := shown[row.SourceTypeId]; ok {
			item.Show = flag
		}
		if !item.Show {
			hidden[item.ID] = item
			continue
		}
		parsed = append(parsed, item)
	}
	view := publicSourceView{
		Groups:  groupPublicTypes(parsed, useRules),
		Hidden:  hidden,
		members: map[int64]publicTypeGroup{},
	}
	indexPublicGroups(view.Groups, view.members)
	return view
}

func publicRulesForSource(sourceID string) bool {
	primary := PickPrimarySourceForCategory()
	if primary == nil || strings.TrimSpace(primary.Id) == "" {
		return true
	}
	return strings.TrimSpace(primary.Id) == sourceID
}

func groupPublicTypes(rows []sourceTypeRow, useRules bool) []publicTypeGroup {
	childrenOf := map[int64][]sourceTypeRow{}
	roots := make([]sourceTypeRow, 0)
	for _, row := range rows {
		if row.Parent > 0 {
			childrenOf[row.Parent] = append(childrenOf[row.Parent], row)
			continue
		}
		roots = append(roots, row)
	}
	groups := groupTypeRows(roots, useRules, true)
	for i := range groups {
		var children []sourceTypeRow
		for _, member := range groups[i].Members {
			children = append(children, childrenOf[member]...)
		}
		groups[i].Children = groupTypeRows(children, useRules, false)
		for j := range groups[i].Children {
			groups[i].Children[j].Parent = groups[i].ID
		}
	}
	return groups
}

func groupTypeRows(rows []sourceTypeRow, useRules bool, root bool) []publicTypeGroup {
	order := make([]string, 0)
	buckets := map[string][]sourceTypeRow{}
	for _, row := range rows {
		name := row.Name
		if useRules {
			if root {
				name = support.NormalizeRootCategoryName(name)
			} else {
				name = support.NormalizeSubCategoryName(name)
			}
		}
		name = strings.TrimSpace(name)
		if name == "" {
			name = row.Name
		}
		if _, ok := buckets[name]; !ok {
			order = append(order, name)
		}
		buckets[name] = append(buckets[name], row)
	}
	groups := make([]publicTypeGroup, 0, len(order))
	for _, name := range order {
		members := buckets[name]
		group := publicTypeGroup{ID: members[0].ID, Name: name, Sort: members[0].Sort, Show: true, Members: make([]int64, 0, len(members))}
		for _, row := range members {
			group.Members = append(group.Members, row.ID)
			if row.ID < group.ID {
				group.ID = row.ID
			}
			if row.Sort < group.Sort {
				group.Sort = row.Sort
			}
		}
		sort.Slice(group.Members, func(i, j int) bool { return group.Members[i] < group.Members[j] })
		groups = append(groups, group)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].Sort != groups[j].Sort {
			return groups[i].Sort < groups[j].Sort
		}
		return groups[i].ID < groups[j].ID
	})
	return groups
}

func indexPublicGroups(groups []publicTypeGroup, byID map[int64]publicTypeGroup) {
	for i := range groups {
		group := groups[i]
		for _, member := range group.Members {
			byID[member] = group
		}
		byID[group.ID] = group
		indexPublicGroups(group.Children, byID)
	}
}
