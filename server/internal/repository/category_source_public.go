package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"
)

// SourceRootTypeIDs 当前采集站前台正在显示的一级分类。id 是该站 type_id，和首页栏目一致。
func SourceRootTypeIDs(sourceID string) []int64 {
	tree, err := GetActiveCategoryTree(sourceID)
	if err != nil {
		return nil
	}
	ids := make([]int64, 0, len(tree.Children))
	for _, child := range tree.Children {
		if child == nil || !child.Show || child.Id <= 0 {
			continue
		}
		ids = append(ids, child.Id)
	}
	return ids
}

// ResolveBannerSourceCategories 排片分类只认首选站自己的一级分类。
// 没选表示不限。旧的整库展示分类编号对不上这个站时，退回该站全部一级分类。
func ResolveBannerSourceCategories(sourceID string, selected []int64) (ids []int64, fellBack bool) {
	roots := SourceRootTypeIDs(sourceID)
	if len(selected) == 0 {
		return roots, false
	}
	allow := make(map[int64]struct{}, len(roots))
	for _, id := range roots {
		allow[id] = struct{}{}
	}
	out := make([]int64, 0, len(selected))
	seen := make(map[int64]struct{}, len(selected))
	for _, id := range selected {
		if _, ok := allow[id]; !ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return roots, true
	}
	return out, false
}

// sourceTypeCategoryTree 前台分类树。节点 id 是当前采集站自己的 type_id。
// 首选站套用分类规则：命中的改名，同一父级下目标名相同的 type 合成一个节点。
// 一级分类被改名后，原名落到目标分类下作为二级筛选。
func sourceTypeCategoryTree(sourceId string) (model.CategoryTree, error) {
	root := model.CategoryTree{
		Id: 0, Pid: -1, Name: "分类信息", Show: true,
		Children: make([]*model.CategoryTree, 0),
	}
	sourceId = strings.TrimSpace(sourceId)
	if sourceId == "" {
		return root, nil
	}
	if db.Mdb == nil {
		return root, errors.New("数据库未连接")
	}

	cacheKey := fmt.Sprintf("%s:src_%s:stvis:r%s:c%s", config.ActiveCategoryTreeKey, sourceId, support.GetRuleVersion(), support.GetCategoryVersion())
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var tree model.CategoryTree
			if json.Unmarshal([]byte(data), &tree) == nil && validSourceTypeTree(tree) {
				return tree, nil
			}
			db.Rdb.Del(db.Cxt, cacheKey)
		}
	}

	view, err := loadPublicSourceTypes(sourceId)
	if err != nil {
		return root, err
	}
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
	return root, nil
}

type sourceTypePlacement struct {
	Sort int
	Show bool
}

// sourceTypePlacements 分类管理里的排序和显示。键是该站 type_id。没有映射的分类沿用来源排序并保持显示。
func sourceTypePlacements(sourceId string) (map[int64]sourceTypePlacement, error) {
	type pair struct {
		SourceTypeId int64 `gorm:"column:source_type_id"`
		Sort         int   `gorm:"column:sort"`
		Show         bool  `gorm:"column:show"`
	}
	var pairs []pair
	if err := db.Mdb.Model(&model.CategoryMapping{}).
		Select("category_mappings.source_type_id, film_category.sort, film_category.show").
		Joins("JOIN film_category ON film_category.id = category_mappings.category_id").
		Where("category_mappings.source_id = ?", sourceId).
		Scan(&pairs).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]sourceTypePlacement, len(pairs))
	for _, pair := range pairs {
		if pair.SourceTypeId <= 0 {
			continue
		}
		if prev, ok := out[pair.SourceTypeId]; ok {
			prev.Show = prev.Show && pair.Show
			if pair.Sort < prev.Sort {
				prev.Sort = pair.Sort
			}
			out[pair.SourceTypeId] = prev
			continue
		}
		out[pair.SourceTypeId] = sourceTypePlacement{Sort: pair.Sort, Show: pair.Show}
	}
	return out, nil
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
// 合并后的成员 type 返回合成节点。隐藏的分类（含规则合并组及其隐藏子类）不返回。
func LookupRootSourceType(sourceId string, typeId int64) *model.CategoryTree {
	if typeId <= 0 {
		return nil
	}
	view, err := loadPublicSourceTypes(sourceId)
	if err != nil {
		return nil
	}
	if !view.publiclyVisible(typeId) {
		return nil
	}
	if group, ok := view.rootByMember(typeId); ok && group.Parent == 0 {
		return group.treeNode(0)
	}
	return nil
}

// PublicSourceTypeIDs 前台这个分类要包含的本站 type_id。
// 一级分类返回自身成员及下属可见子类成员；二级分类返回自身可见合并成员。
// 隐藏的分类（含规则合并组及其隐藏子类）不返回。未建组的 type 只返回自己。
func PublicSourceTypeIDs(sourceID, field string, typeID int64) []int64 {
	if typeID <= 0 {
		return nil
	}
	view, err := loadPublicSourceTypes(sourceID)
	if err != nil {
		return nil
	}
	if !view.publiclyVisible(typeID) {
		return nil
	}
	if field == "cid" {
		if group, ok := view.groupByMember(typeID); ok {
			if group.Parent == 0 && len(group.Children) > 0 {
				return []int64{typeID}
			}
			return group.publicMembers(view.Hidden)
		}
		return []int64{typeID}
	}
	if group, ok := view.rootByMember(typeID); ok {
		return group.publicTypeIDs(view.Hidden)
	}
	return []int64{typeID}
}

// PublicSourceTypeName 当前站这个 type_id 的前台名。合并后的成员共用合成名。隐藏分类为空。
func PublicSourceTypeName(sourceID string, typeID int64) string {
	if typeID <= 0 {
		return ""
	}
	view, err := loadPublicSourceTypes(sourceID)
	if err != nil {
		return ""
	}
	if !view.publiclyVisible(typeID) {
		return ""
	}
	if group, ok := view.groupByMember(typeID); ok {
		return strings.TrimSpace(group.Name)
	}
	return ""
}

// PublicSourceChildNodes 前台一级分类下的子类。已套规则，不含隐藏。
func PublicSourceChildNodes(sourceID string, parentTypeID int64) []*model.CategoryTree {
	view, err := loadPublicSourceTypes(sourceID)
	if err != nil {
		return nil
	}
	if !view.publiclyVisible(parentTypeID) {
		return []*model.CategoryTree{}
	}
	group, ok := view.rootByMember(parentTypeID)
	if !ok {
		return []*model.CategoryTree{}
	}
	return group.treeNode(0).Children
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

func (g publicTypeGroup) publicMembers(hidden map[int64]sourceTypeRow) []int64 {
	out := make([]int64, 0, len(g.Members))
	for _, id := range g.Members {
		if id <= 0 {
			continue
		}
		if _, skip := hidden[id]; skip {
			continue
		}
		out = append(out, id)
	}
	return out
}

func (g publicTypeGroup) publicTypeIDs(hidden map[int64]sourceTypeRow) []int64 {
	skip := make(map[int64]struct{}, len(hidden)+len(g.Children)*2)
	for id := range hidden {
		skip[id] = struct{}{}
	}
	for i := range g.Children {
		if g.Children[i].Show {
			continue
		}
		for _, id := range g.Children[i].Members {
			if id > 0 {
				skip[id] = struct{}{}
			}
		}
	}
	out := make([]int64, 0, len(g.Members)+len(g.Children)*2)
	seen := make(map[int64]struct{}, len(g.Members)+len(g.Children)*2)
	add := func(id int64) {
		if id <= 0 {
			return
		}
		if _, blocked := skip[id]; blocked {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range g.Members {
		add(id)
	}
	for i := range g.Children {
		if !g.Children[i].Show {
			continue
		}
		for _, id := range g.Children[i].Members {
			add(id)
		}
	}
	return out
}

func (g publicTypeGroup) treeNode(parentID int64) *model.CategoryTree {
	node := &model.CategoryTree{
		Id:       g.ID,
		Pid:      parentID,
		Name:     g.Name,
		Show:     g.Show,
		Sort:     g.Sort,
		Children: make([]*model.CategoryTree, 0, len(g.Children)),
	}
	for i := range g.Children {
		if !g.Children[i].Show {
			continue
		}
		node.Children = append(node.Children, g.Children[i].treeNode(g.ID))
	}
	return node
}

type publicSourceView struct {
	Groups  []publicTypeGroup
	members map[int64]publicTypeGroup
	roots   map[int64]publicTypeGroup
	Hidden  map[int64]sourceTypeRow
}

func (v publicSourceView) groupByMember(typeID int64) (publicTypeGroup, bool) {
	group, ok := v.members[typeID]
	return group, ok
}

func (v publicSourceView) rootByMember(typeID int64) (publicTypeGroup, bool) {
	group, ok := v.roots[typeID]
	return group, ok
}

func (v publicSourceView) typeHidden(typeID int64) bool {
	_, ok := v.Hidden[typeID]
	return ok
}

func (v publicSourceView) publiclyVisible(typeID int64) bool {
	if v.typeHidden(typeID) {
		return false
	}
	if group, ok := v.groupByMember(typeID); ok {
		if !group.Show {
			return false
		}
		if group.Parent == 0 {
			return true
		}
		root, ok := v.roots[group.Parent]
		return ok && root.Show
	}
	if root, ok := v.rootByMember(typeID); ok {
		return root.Show
	}
	return true
}

type publicSourceCacheEntry struct {
	key  string
	view publicSourceView
}

var (
	publicSourceCacheMu sync.Mutex
	publicSourceCache   = map[string]publicSourceCacheEntry{}
)

func loadPublicSourceTypes(sourceID string) (publicSourceView, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || db.Mdb == nil {
		return publicSourceView{}, nil
	}
	cacheKey := sourceID + ":" + support.GetRuleVersion() + ":" + support.GetCategoryVersion()
	publicSourceCacheMu.Lock()
	if entry, ok := publicSourceCache[sourceID]; ok && entry.key == cacheKey {
		view := entry.view
		publicSourceCacheMu.Unlock()
		return view, nil
	}
	publicSourceCacheMu.Unlock()

	view, err := buildPublicSourceView(sourceID)
	if err != nil {
		return publicSourceView{}, err
	}
	publicSourceCacheMu.Lock()
	publicSourceCache[sourceID] = publicSourceCacheEntry{key: cacheKey, view: view}
	publicSourceCacheMu.Unlock()
	return view, nil
}

func buildPublicSourceView(sourceID string) (publicSourceView, error) {
	var rows []model.SourceCategory
	if err := db.Mdb.Where("source_id = ?", sourceID).
		Order("depth ASC, sort ASC, source_type_id ASC").
		Find(&rows).Error; err != nil {
		return publicSourceView{}, err
	}
	placements, err := sourceTypePlacements(sourceID)
	if err != nil {
		return publicSourceView{}, err
	}
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
		if placement, ok := placements[row.SourceTypeId]; ok {
			item.Sort = placement.Sort
			item.Show = placement.Show
		}
		if !item.Show {
			hidden[item.ID] = item
		}
		parsed = append(parsed, item)
	}
	view := publicSourceView{
		Groups:  groupPublicTypes(parsed, sourceID),
		Hidden:  hidden,
		members: map[int64]publicTypeGroup{},
		roots:   map[int64]publicTypeGroup{},
	}
	indexPublicGroups(view.Groups, view.members, view.roots)
	return view, nil
}
