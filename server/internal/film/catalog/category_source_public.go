package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/taxonomy"
	"strings"
	"time"
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

	cacheKey := fmt.Sprintf("%s:src_%s:stvis:r%s:c%s", config.ActiveCategoryTreeKey, sourceId, taxonomy.GetRuleVersion(), taxonomy.GetCategoryVersion())
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

type SourceTypePlacement struct {
	Sort int
	Show bool
}

// SourceTypePlacements 分类管理里的排序和显示。键是该站 type_id。没有映射的分类沿用来源排序并保持显示。
func SourceTypePlacements(sourceId string) (map[int64]SourceTypePlacement, error) {
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
	out := make(map[int64]SourceTypePlacement, len(pairs))
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
		out[pair.SourceTypeId] = SourceTypePlacement{Sort: pair.Sort, Show: pair.Show}
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
			if group.Synthetic {
				return group.publicTypeIDs(view.Hidden)
			}
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

// PublicSourceTypeVisible 当前站这个 type_id 对前台是否可见。隐藏组、隐藏子类、隐藏父级下的成员都为 false。
func PublicSourceTypeVisible(sourceID string, typeID int64) bool {
	if typeID <= 0 {
		return false
	}
	view, err := loadPublicSourceTypes(sourceID)
	if err != nil {
		return false
	}
	return view.publiclyVisible(typeID)
}

// PublicSourceTypeVisibility 一次读出当前站的前台分类可见性，返回的函数可在循环里反复调用。
// 逐条调用 PublicSourceTypeVisible 每次都会读两次 Redis 版本号，批量筛选时会被放大成上万次往返。
func PublicSourceTypeVisibility(sourceID string) func(typeID int64) bool {
	view, err := loadPublicSourceTypes(sourceID)
	if err != nil {
		return func(int64) bool { return false }
	}
	return func(typeID int64) bool {
		return typeID > 0 && view.publiclyVisible(typeID)
	}
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

// PublicSourceCategoryNames 返回当前站目标分类及其可见子类的展示名称列表。
func PublicSourceCategoryNames(sourceID string, typeID int64) []string {
	if typeID <= 0 {
		return nil
	}
	view, err := loadPublicSourceTypes(sourceID)
	if err != nil || !view.publiclyVisible(typeID) {
		return nil
	}
	group, ok := view.rootByMember(typeID)
	if !ok {
		if g, exists := view.groupByMember(typeID); exists {
			group = g
			ok = true
		}
	}
	if !ok {
		return nil
	}
	seen := make(map[string]struct{}, 1+len(group.Children))
	out := make([]string, 0, 1+len(group.Children))
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, exists := seen[name]; exists {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	add(group.Name)
	for i := range group.Children {
		if group.Children[i].Show {
			add(group.Children[i].Name)
		}
	}
	return out
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
