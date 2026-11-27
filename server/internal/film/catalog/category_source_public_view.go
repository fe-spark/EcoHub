package catalog

import (
	"crypto/sha256"
	"fmt"
	"golang.org/x/sync/singleflight"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/taxonomy"
	"sort"
	"strings"
	"sync"

	"gorm.io/gorm"
)

type sourceTypeRow struct {
	ID     int64
	Parent int64
	Name   string
	Sort   int
	Show   bool
}

type publicTypeGroup struct {
	ID            int64
	CategoryID    int64
	Parent        int64
	Name          string
	Sort          int
	Show          bool
	Synthetic     bool
	Members       []int64
	RootMemberIDs []int64
	Children      []publicTypeGroup
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
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
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
	publicSourceSF      singleflight.Group
)

// InvalidatePublicSourceCache 清理站点公开分类内存缓存；不传参时清空全部。
func InvalidatePublicSourceCache(sourceIDs ...string) {
	publicSourceCacheMu.Lock()
	defer publicSourceCacheMu.Unlock()
	if len(sourceIDs) == 0 {
		clear(publicSourceCache)
		return
	}
	for _, id := range sourceIDs {
		delete(publicSourceCache, strings.TrimSpace(id))
	}
}

func publicSourceCacheVersionKey(sourceID string) string {
	return sourceID + ":" + taxonomy.GetRuleVersion() + ":" + taxonomy.GetCategoryVersion()
}

func loadPublicSourceTypes(sourceID string) (publicSourceView, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || db.Mdb == nil {
		return publicSourceView{}, nil
	}
	cacheKey := publicSourceCacheVersionKey(sourceID)
	publicSourceCacheMu.Lock()
	if entry, ok := publicSourceCache[sourceID]; ok && entry.key == cacheKey {
		view := entry.view
		publicSourceCacheMu.Unlock()
		return view, nil
	}
	publicSourceCacheMu.Unlock()

	res, err, _ := publicSourceSF.Do(cacheKey, func() (any, error) {
		view, err := buildPublicSourceView(sourceID)
		if err != nil {
			return publicSourceView{}, err
		}
		if publicSourceCacheVersionKey(sourceID) == cacheKey {
			publicSourceCacheMu.Lock()
			// 仅当写回时全局规则与分类版本仍未变化才写入缓存，防止慢构建覆盖新版视图
			if publicSourceCacheVersionKey(sourceID) == cacheKey {
				if _, exists := publicSourceCache[sourceID]; !exists && len(publicSourceCache) >= 256 {
					clear(publicSourceCache)
				}
				publicSourceCache[sourceID] = publicSourceCacheEntry{key: cacheKey, view: view}
			}
			publicSourceCacheMu.Unlock()
		}
		return view, nil
	})
	if err != nil {
		return publicSourceView{}, err
	}
	return res.(publicSourceView), nil
}

func buildPublicSourceRawRows(sourceID string) ([]sourceTypeRow, []publicTypeGroup, map[int64]sourceTypeRow, error) {
	var rows []model.SourceCategory
	if err := db.Mdb.Where("source_id = ?", sourceID).
		Order("depth ASC, sort ASC, source_type_id ASC").
		Find(&rows).Error; err != nil {
		return nil, nil, nil, err
	}
	placements, err := SourceTypePlacements(sourceID)
	if err != nil {
		return nil, nil, nil, err
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
	groups := groupPublicTypes(parsed, sourceID)
	return parsed, groups, hidden, nil
}

func buildPublicSourceView(sourceID string) (publicSourceView, error) {
	parsed, groups, hidden, err := buildPublicSourceRawRows(sourceID)
	if err != nil {
		return publicSourceView{}, err
	}
	attachSyntheticRootGroupsReadOnly(sourceID, parsed, groups)
	sortPublicGroups(groups)
	view := publicSourceView{
		Groups:  groups,
		Hidden:  hidden,
		members: map[int64]publicTypeGroup{},
		roots:   map[int64]publicTypeGroup{},
	}
	indexPublicGroups(view.Groups, view.members, view.roots)
	return view, nil
}

// attachSyntheticRootGroupsReadOnly 仅从已落库的 film_category 回填合成一级分组状态，不在读链路做任何 DB 写操作。
func attachSyntheticRootGroupsReadOnly(sourceID string, rows []sourceTypeRow, groups []publicTypeGroup) {
	if db.Mdb == nil || strings.TrimSpace(sourceID) == "" {
		return
	}
	hasSynthetic := false
	for i := range groups {
		if groups[i].Synthetic {
			hasSynthetic = true
			break
		}
	}
	if !hasSynthetic {
		return
	}
	var existing []model.Category
	_ = db.Mdb.Where("stable_key LIKE ?", syntheticRootPrefix(sourceID)+"%").Find(&existing).Error
	existingByKey := make(map[string]model.Category, len(existing))
	for _, cat := range existing {
		existingByKey[cat.StableKey] = cat
	}
	usedIDs := make(map[int64]struct{}, len(rows))
	for _, row := range rows {
		if row.ID > 0 {
			usedIDs[row.ID] = struct{}{}
		}
	}
	for i := range groups {
		if !groups[i].Synthetic {
			continue
		}
		key := syntheticRootStableKey(sourceID, groups[i].Name)
		if cat, ok := existingByKey[key]; ok {
			groups[i].CategoryID = cat.Id
			groups[i].Show = cat.Show
			groups[i].Sort = cat.Sort
			groups[i].ID = allocateSyntheticTypeID(cat.Id, usedIDs)
		} else {
			groups[i].ID = allocateSyntheticTypeID(int64(i+1), usedIDs)
		}
		for j := range groups[i].Children {
			groups[i].Children[j].Parent = groups[i].ID
		}
	}
}

// EnsureSyntheticRootGroupsForSource 在写锁或同步流程中维护规则合成一级分组对应的 film_category 行。
func EnsureSyntheticRootGroupsForSource(sourceID string) {
	sourceID = strings.TrimSpace(sourceID)
	if db.Mdb == nil || sourceID == "" {
		return
	}
	rows, groups, _, err := buildPublicSourceRawRows(sourceID)
	if err != nil {
		return
	}
	ensureSyntheticRootGroupsTx(sourceID, rows, groups)
}

// SyntheticRootTypeOffset 规则合成一级分类的起始虚拟 ID 偏移量，避免与采集站真实 type_id 冲突。
const SyntheticRootTypeOffset int64 = 100_000_000

const syntheticRootTypeOffset = SyntheticRootTypeOffset

func syntheticRootPrefix(sourceID string) string {
	return "rule:root:" + strings.TrimSpace(sourceID) + ":"
}

func syntheticRootStableKey(sourceID, groupName string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(groupName)))
	return fmt.Sprintf("%s%x", syntheticRootPrefix(sourceID), sum[:8])
}

func allocateSyntheticTypeID(categoryID int64, used map[int64]struct{}) int64 {
	id := syntheticRootTypeOffset + categoryID
	if id <= 0 {
		id = syntheticRootTypeOffset + 1
	}
	for {
		if _, exists := used[id]; !exists {
			used[id] = struct{}{}
			return id
		}
		id++
	}
}

// ensureSyntheticRootGroupsTx 在单事务中为规则新合成的一级分组维护独立的 film_category 行，
// 避免借用首个子项 ID 导致后台父子 rowKey 冲突或子项被隐藏吞并。
func ensureSyntheticRootGroupsTx(sourceID string, rows []sourceTypeRow, groups []publicTypeGroup) {
	if db.Mdb == nil || strings.TrimSpace(sourceID) == "" {
		return
	}
	prefix := syntheticRootPrefix(sourceID)
	var desiredKeys = make(map[string]struct{})
	for i := range groups {
		if groups[i].Synthetic {
			desiredKeys[syntheticRootStableKey(sourceID, groups[i].Name)] = struct{}{}
		}
	}
	usedIDs := make(map[int64]struct{}, len(rows))
	for _, row := range rows {
		if row.ID > 0 {
			usedIDs[row.ID] = struct{}{}
		}
	}

	changed := false
	_ = db.Mdb.Transaction(func(tx *gorm.DB) error {
		var existing []model.Category
		if err := tx.Where("stable_key LIKE ?", prefix+"%").Find(&existing).Error; err != nil {
			return err
		}
		existingByKey := make(map[string]model.Category, len(existing))
		for _, cat := range existing {
			existingByKey[cat.StableKey] = cat
		}
		if len(desiredKeys) == 0 && len(existingByKey) == 0 {
			return nil
		}

		staleIDs := make([]int64, 0)
		for key, cat := range existingByKey {
			if _, keep := desiredKeys[key]; !keep && cat.Id > 0 {
				staleIDs = append(staleIDs, cat.Id)
			}
		}
		if len(staleIDs) > 0 {
			var mapped []int64
			_ = tx.Model(&model.CategoryMapping{}).
				Where("category_id IN ?", staleIDs).
				Distinct("category_id").
				Pluck("category_id", &mapped).Error
			inUse := make(map[int64]struct{}, len(mapped))
			for _, id := range mapped {
				inUse[id] = struct{}{}
			}
			dropIDs := make([]int64, 0, len(staleIDs))
			for _, id := range staleIDs {
				if _, ok := inUse[id]; !ok {
					dropIDs = append(dropIDs, id)
				}
			}
			if len(dropIDs) > 0 && tx.Where("id IN ?", dropIDs).Delete(&model.Category{}).Error == nil {
				changed = true
			}
		}

		for i := range groups {
			if !groups[i].Synthetic {
				continue
			}
			key := syntheticRootStableKey(sourceID, groups[i].Name)
			cat, ok := existingByKey[key]
			if !ok {
				cat = model.Category{
					Pid:       0,
					Name:      groups[i].Name,
					StableKey: key,
					Show:      true,
					Sort:      groups[i].Sort,
				}
				if err := tx.Create(&cat).Error; err != nil {
					var again model.Category
					if tx.Where("stable_key = ?", key).First(&again).Error != nil {
						continue
					}
					cat = again
				} else {
					changed = true
				}
				existingByKey[key] = cat
			} else if cat.Name != groups[i].Name {
				if tx.Model(&model.Category{}).Where("id = ?", cat.Id).Update("name", groups[i].Name).Error == nil {
					cat.Name = groups[i].Name
					changed = true
				}
			}
			groups[i].CategoryID = cat.Id
			groups[i].Show = cat.Show
			groups[i].Sort = cat.Sort
			groups[i].ID = allocateSyntheticTypeID(cat.Id, usedIDs)
		}
		return nil
	})
	if changed {
		taxonomy.RefreshCategoryCache()
	}
}
