package repository

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

type categoryPlacement struct {
	Id    int64
	Pid   int64
	Sort  int
	Depth int
}

type sourceCategoryPlacement struct {
	SourceTypeId       int64
	ParentSourceTypeId int64
	Name               string
	Sort               int
	Depth              int
}

type categoryTreeWalkNode struct {
	Node     *model.CategoryTree
	ParentId int64
	Depth    int
	Sort     int
}

func BuildCategoryStableKey(pid int64, name string) string {
	return support.BuildCategoryStableKey(pid, name)
}

func GetCategoryStableKeyByID(id int64) string {
	return support.GetCategoryStableKeyByID(id)
}

func GetCategoryByID(id int64) *model.Category {
	if id <= 0 {
		return nil
	}
	var category model.Category
	if err := db.Mdb.Where("id = ?", id).First(&category).Error; err != nil {
		return nil
	}
	return &category
}

func GetCategoryByStableKey(stableKey string) *model.Category {
	stableKey = strings.TrimSpace(stableKey)
	if stableKey == "" {
		return nil
	}
	var category model.Category
	if err := db.Mdb.Where("stable_key = ?", stableKey).First(&category).Error; err != nil {
		return nil
	}
	return &category
}

func ResolveCategoryID(id int64) int64 {
	return support.ResolveCategoryID(id)
}

func touchCategoryVersion() {
	support.TouchCategoryVersion()
}

func GetCategoryVersion() string {
	return support.GetCategoryVersion()
}

func GetVersionedIndexPageCacheKey() string {
	return support.GetVersionedIndexPageCacheKey()
}

func ClearIndexPageCache() {
	support.ClearIndexPageCache()
}

// RefreshCategoryCache 用于重新加载基础映射映射到内存
func RefreshCategoryCache() {
	support.RefreshCategoryCache()
}

// GetRootId 获取分类的顶级根 ID (通过内存递归映射)
func GetRootId(id int64) int64 {
	return support.GetRootId(id)
}

// IsRootCategory 判断是否为根分类 (Pid 为 0 的大类)
func IsRootCategory(id int64) bool {
	return support.IsRootCategory(id)
}

// GetParentId 获取父类 ID
func GetParentId(id int64) int64 {
	return support.GetParentId(id)
}

func SaveCategoryTree(sourceId string, tree *model.CategoryTree) error {
	return saveCategoryTree(sourceId, tree, true, false)
}

func ResetCategoryTree(sourceId string, tree *model.CategoryTree) error {
	return saveCategoryTree(sourceId, tree, false, false)
}

func saveCategoryTree(sourceId string, tree *model.CategoryTree, preserveBusinessFields bool, skipRebuild bool) error {
	sourceId = strings.TrimSpace(sourceId)
	if sourceId == "" {
		return fmt.Errorf("source id 不能为空")
	}
	if tree == nil {
		return nil
	}

	plans := make([]sourceCategoryPlacement, 0)
	if err := flattenSourceCategoryPlacements(tree.Children, 0, 0, &plans); err != nil {
		return err
	}
	return saveCategoryPlans(sourceId, plans, preserveBusinessFields, skipRebuild)
}

func saveCategoryPlans(sourceId string, plans []sourceCategoryPlacement, preserveBusinessFields bool, skipRebuild bool) error {
	sourceId = strings.TrimSpace(sourceId)
	if sourceId == "" {
		return fmt.Errorf("source id 不能为空")
	}

	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		var oldCategories []model.Category
		if err := tx.Order("pid ASC, sort ASC, id ASC").Find(&oldCategories).Error; err != nil {
			return err
		}
		currentMap := make(map[int64]model.Category, len(oldCategories))
		stableKeyToCategory := make(map[string]model.Category, len(oldCategories))
		categoryByParentName := make(map[string]model.Category, len(oldCategories))
		for _, item := range oldCategories {
			currentMap[item.Id] = item
			categoryByParentName[categoryParentNameKey(item.Pid, item.Name)] = item
			stableKey := strings.TrimSpace(item.StableKey)
			if stableKey != "" {
				stableKeyToCategory[stableKey] = item
			}
		}

		var oldMappings []model.CategoryMapping
		if err := tx.Where("source_id = ?", sourceId).Find(&oldMappings).Error; err != nil {
			return err
		}
		existingCategoryIDs := make(map[int64]struct{}, len(oldMappings))
		existingBySourceType := make(map[int64]int64, len(oldMappings))
		for _, item := range oldMappings {
			existingBySourceType[item.SourceTypeId] = item.CategoryId
			existingCategoryIDs[item.CategoryId] = struct{}{}
		}

		if !preserveBusinessFields {
			existingCategoryIDs = make(map[int64]struct{})
			existingBySourceType = make(map[int64]int64)
			if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.CategoryMapping{}).Error; err != nil {
				return err
			}
			if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.Category{}).Error; err != nil {
				return err
			}
			if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.SourceCategory{}).Error; err != nil {
				return err
			}
			if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.SearchTagItem{}).Error; err != nil {
				return err
			}
			currentMap = make(map[int64]model.Category)
			stableKeyToCategory = make(map[string]model.Category)
			categoryByParentName = make(map[string]model.Category)
		}

		if preserveBusinessFields {
			if err := tx.Where("source_id = ?", sourceId).Delete(&model.SourceCategory{}).Error; err != nil {
				return err
			}
		}
		rawRows := make([]model.SourceCategory, 0, len(plans))
		for _, plan := range plans {
			rawRows = append(rawRows, model.SourceCategory{
				SourceId:           sourceId,
				SourceTypeId:       plan.SourceTypeId,
				ParentSourceTypeId: plan.ParentSourceTypeId,
				RawName:            strings.TrimSpace(plan.Name),
				Sort:               plan.Sort,
				Depth:              plan.Depth,
			})
		}
		if len(rawRows) > 0 {
			if err := tx.Create(&rawRows).Error; err != nil {
				return err
			}
		}

		sourceTypeToCategory := make(map[int64]int64, len(plans))
		sourceTypeToDisplayKey := make(map[int64]string, len(plans))
		claimedCategoryIDs := make(map[int64]struct{}, len(plans))
		seenSourceType := make(map[int64]struct{}, len(plans))
		for _, plan := range plans {
			if _, ok := seenSourceType[plan.SourceTypeId]; ok {
				return fmt.Errorf("来源分类重复: %d", plan.SourceTypeId)
			}
			seenSourceType[plan.SourceTypeId] = struct{}{}
			normalizedName := normalizeCategoryPlanName(plan)

			pid := int64(0)
			if plan.ParentSourceTypeId > 0 {
				parentId, ok := sourceTypeToCategory[plan.ParentSourceTypeId]
				if !ok {
					return fmt.Errorf("来源父分类不存在: %d", plan.ParentSourceTypeId)
				}
				pid = parentId
			}

			stableKey := buildDisplayCategoryStableKey(pid, normalizedName, sourceTypeToDisplayKey[plan.ParentSourceTypeId])
			if stableKey == "" {
				return fmt.Errorf("来源分类稳定标识生成失败: %d", plan.SourceTypeId)
			}

			if existingCategory, ok := categoryByParentName[categoryParentNameKey(pid, normalizedName)]; ok {
				updates := map[string]any{
					"pid":        pid,
					"name":       normalizedName,
					"stable_key": stableKey,
				}
				if !preserveBusinessFields {
					updates["sort"] = plan.Sort
					updates["show"] = true
					updates["alias"] = ""
				}
				if err := tx.Model(&model.Category{}).Where("id = ?", existingCategory.Id).Updates(updates).Error; err != nil {
					return err
				}
				existingCategory.Pid = pid
				existingCategory.Name = normalizedName
				existingCategory.StableKey = stableKey
				currentMap[existingCategory.Id] = existingCategory
				stableKeyToCategory[stableKey] = existingCategory
				categoryByParentName[categoryParentNameKey(pid, normalizedName)] = existingCategory
				sourceTypeToCategory[plan.SourceTypeId] = existingCategory.Id
				sourceTypeToDisplayKey[plan.SourceTypeId] = stableKey
				claimedCategoryIDs[existingCategory.Id] = struct{}{}
				continue
			}

			if existingCategory, ok := stableKeyToCategory[stableKey]; ok {
				sourceTypeToCategory[plan.SourceTypeId] = existingCategory.Id
				sourceTypeToDisplayKey[plan.SourceTypeId] = stableKey
				claimedCategoryIDs[existingCategory.Id] = struct{}{}
				continue
			}

			categoryId := existingBySourceType[plan.SourceTypeId]
			if categoryId > 0 {
				if _, claimed := claimedCategoryIDs[categoryId]; claimed {
					categoryId = 0
				}
			}
			if categoryId > 0 {
				existingCategory, ok := currentMap[categoryId]
				if !ok {
					return fmt.Errorf("已有业务分类不存在: %d", categoryId)
				}
				updates := map[string]any{
					"pid":        pid,
					"name":       normalizedName,
					"stable_key": stableKey,
				}
				if preserveBusinessFields {
					updates["sort"] = existingCategory.Sort
				} else {
					updates["sort"] = plan.Sort
					updates["show"] = true
					updates["alias"] = ""
				}
				if err := tx.Model(&model.Category{}).Where("id = ?", categoryId).Updates(updates).Error; err != nil {
					return err
				}
				existingCategory.Pid = pid
				existingCategory.Name = normalizedName
				existingCategory.StableKey = stableKey
				currentMap[categoryId] = existingCategory
				stableKeyToCategory[stableKey] = existingCategory
				categoryByParentName[categoryParentNameKey(pid, normalizedName)] = existingCategory
			} else {
				category := model.Category{Pid: pid, Name: normalizedName, StableKey: stableKey, Show: true, Sort: plan.Sort}
				if err := tx.Create(&category).Error; err != nil {
					return err
				}
				categoryId = category.Id
				currentMap[categoryId] = category
				stableKeyToCategory[stableKey] = category
				categoryByParentName[categoryParentNameKey(pid, normalizedName)] = category
			}
			claimedCategoryIDs[categoryId] = struct{}{}
			sourceTypeToCategory[plan.SourceTypeId] = categoryId
			sourceTypeToDisplayKey[plan.SourceTypeId] = stableKey
		}

		if preserveBusinessFields {
			if err := tx.Where("source_id = ?", sourceId).Delete(&model.CategoryMapping{}).Error; err != nil {
				return err
			}
		}
		mappings := make([]model.CategoryMapping, 0, len(plans))
		activeCategoryIDs := make(map[int64]struct{}, len(plans))
		for _, plan := range plans {
			categoryId := sourceTypeToCategory[plan.SourceTypeId]
			activeCategoryIDs[categoryId] = struct{}{}
			mappings = append(mappings, model.CategoryMapping{
				SourceId:     sourceId,
				SourceTypeId: plan.SourceTypeId,
				CategoryId:   categoryId,
			})
		}
		if len(mappings) > 0 {
			if err := tx.Create(&mappings).Error; err != nil {
				return err
			}
		}

		if preserveBusinessFields {
			staleCategoryIDs := make([]int64, 0)
			for categoryId := range existingCategoryIDs {
				if _, ok := activeCategoryIDs[categoryId]; ok {
					continue
				}
				staleCategoryIDs = append(staleCategoryIDs, categoryId)
			}
			if len(staleCategoryIDs) > 0 {
				if err := tx.Where("id IN ?", staleCategoryIDs).Delete(&model.Category{}).Error; err != nil {
					return err
				}
				for _, categoryId := range staleCategoryIDs {
					delete(currentMap, categoryId)
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	if !skipRebuild {
		MarkCategoryChanged()
	}
	return nil
}

func loadSourceCategoryPlacementsBySourceIDs(sourceIDs []string) (map[string][]sourceCategoryPlacement, error) {
	if len(sourceIDs) == 0 {
		return map[string][]sourceCategoryPlacement{}, nil
	}

	var rows []model.SourceCategory
	if err := db.Mdb.Where("source_id IN ?", sourceIDs).
		Order("source_id ASC, depth ASC, parent_source_type_id ASC, sort ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	plansBySource := make(map[string][]sourceCategoryPlacement, len(sourceIDs))
	for _, row := range rows {
		sourceID := strings.TrimSpace(row.SourceId)
		if sourceID == "" {
			continue
		}
		name := strings.TrimSpace(row.RawName)
		if name == "" {
			return nil, fmt.Errorf("来源分类名称不能为空: %d", row.SourceTypeId)
		}
		plansBySource[sourceID] = append(plansBySource[sourceID], sourceCategoryPlacement{
			SourceTypeId:       row.SourceTypeId,
			ParentSourceTypeId: row.ParentSourceTypeId,
			Name:               name,
			Sort:               row.Sort,
			Depth:              row.Depth,
		})
	}
	return plansBySource, nil
}

func RefreshFutureCategoryMappingsFromSourceCategories() error {
	// 这里只刷新 categories/category_mappings/cacheSourceMap，不回刷已经固化的历史 film_index。
	// 历史影片在查询时通过最新来源映射自然归入新展示分组。
	var sourceIDs []string
	if err := db.Mdb.Model(&model.FilmSource{}).Where("state = ? AND grade = ?", true, model.MasterCollect).Pluck("id", &sourceIDs).Error; err != nil {
		return err
	}
	plansBySource, err := loadSourceCategoryPlacementsBySourceIDs(sourceIDs)
	if err != nil {
		return err
	}
	for _, sourceID := range sourceIDs {
		plans := plansBySource[sourceID]
		if len(plans) == 0 {
			continue
		}
		if err := saveCategoryPlans(sourceID, plans, true, true); err != nil {
			return err
		}
	}
	RefreshCategoryCache()
	ReloadMappingRules()
	touchCategoryVersion()
	return nil
}

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

func normalizeCategoryPlanName(plan sourceCategoryPlacement) string {
	name := strings.TrimSpace(plan.Name)
	if name == "" {
		return ""
	}
	if plan.ParentSourceTypeId == 0 {
		return support.NormalizeRootCategoryName(name)
	}
	return support.NormalizeSubCategoryName(name)
}

func categoryParentNameKey(pid int64, name string) string {
	return fmt.Sprintf("%d:%s", pid, strings.TrimSpace(name))
}

func buildDisplayCategoryStableKey(pid int64, name string, parentKey string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if pid == 0 {
		return fmt.Sprintf("display:root:%s", name)
	}
	parentKey = strings.TrimSpace(parentKey)
	if parentKey == "" {
		parentKey = support.GetCategoryStableKeyByID(pid)
	}
	if parentKey == "" {
		return fmt.Sprintf("display:sub:%d:%s", pid, name)
	}
	return fmt.Sprintf("%s/%s", parentKey, name)
}

func walkTwoLevelCategoryTree(nodes []*model.CategoryTree, parentId int64, depth int, visit func(item categoryTreeWalkNode) error) error {
	if len(nodes) == 0 {
		return nil
	}
	if depth > 1 {
		return fmt.Errorf("分类层级最多支持两层")
	}

	for index, node := range nodes {
		if err := visit(categoryTreeWalkNode{
			Node:     node,
			ParentId: parentId,
			Depth:    depth,
			Sort:     index + 1,
		}); err != nil {
			return err
		}
		if err := walkTwoLevelCategoryTree(node.Children, node.Id, depth+1, visit); err != nil {
			return err
		}
	}

	return nil
}

func flattenSourceCategoryPlacements(nodes []*model.CategoryTree, parentId int64, depth int, out *[]sourceCategoryPlacement) error {
	return walkTwoLevelCategoryTree(nodes, parentId, depth, func(item categoryTreeWalkNode) error {
		node := item.Node
		if node == nil || node.Id <= 0 {
			return fmt.Errorf("来源分类数据异常")
		}
		name := strings.TrimSpace(node.Name)
		if name == "" {
			return fmt.Errorf("来源分类名称不能为空")
		}
		*out = append(*out, sourceCategoryPlacement{
			SourceTypeId:       node.Id,
			ParentSourceTypeId: item.ParentId,
			Name:               name,
			Sort:               item.Sort,
			Depth:              item.Depth,
		})
		return nil
	})
}

// buildTreeHelper 内部辅助函数：直接从列表构建树形结构内存模型
func buildTreeHelper() model.CategoryTree {
	var allList []model.Category
	db.Mdb.Order("pid ASC, sort ASC, id ASC").Find(&allList)

	nodes := make(map[int64]*model.CategoryTree)
	root := model.CategoryTree{
		Id: 0, Pid: -1, Name: "分类信息", Show: true,
		Children: make([]*model.CategoryTree, 0),
	}

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

// GetActiveCategoryTree 获取仅包含有影视内容的分类树副本 (实时查库 + Redis 缓存)
func GetActiveCategoryTree() model.CategoryTree {
	// 1. 尝试从 Redis 获取
	if data, err := db.Rdb.Get(db.Cxt, config.ActiveCategoryTreeKey).Result(); err == nil && data != "" {
		var tree model.CategoryTree
		if json.Unmarshal([]byte(data), &tree) == nil && isValidActiveCategoryTree(tree) {
			return tree
		}
		db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)
	}

	// 2. 获取活跃的 Pid (MainCategory) 和 Cid (Category)
	var activeCids []int64
	db.Mdb.Table(model.TableFilmIndex).Distinct("cid").Pluck("cid", &activeCids)
	activeCidMap := make(map[int64]bool)
	for _, id := range activeCids {
		activeCidMap[id] = true
	}

	var activePids []int64
	db.Mdb.Table(model.TableFilmIndex).Distinct("pid").Pluck("pid", &activePids)
	activePidMap := make(map[int64]bool)
	for _, id := range activePids {
		activePidMap[id] = true
	}

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

	// 第二遍：处理子类并更新父大类的活跃状态
	for _, c := range allList {
		if activeCidMap[c.Id] {
			if c.Pid == 0 {
				// 本身就是大类，直接标记活跃
				activePidMap[c.Id] = true
			} else if parent, ok := nodes[c.Pid]; ok {
				parent.Children = append(parent.Children, nodes[c.Id])
				activePidMap[c.Pid] = true
			}
		}
	}

	// 第三遍：收集活跃的大类到根节点下
	for _, c := range allList {
		if c.Pid != 0 {
			continue
		}
		node := nodes[c.Id]
		if activePidMap[c.Id] || len(node.Children) > 0 {
			root.Children = append(root.Children, node)
		}
	}
	sortRootCategories(root.Children)

	// 7. 写入 Redis 缓存 (1小时)
	if data, err := json.Marshal(root); err == nil {
		db.Rdb.Set(db.Cxt, config.ActiveCategoryTreeKey, string(data), time.Hour)
	}

	return root
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

// ClearCategoryCache 清除分类相关缓存，不触碰已固化的搜索标签。
func ClearCategoryCache() {
	db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)
	RefreshCategoryCache()
}

func MarkCategoryChanged() {
	ClearCategoryCache()
	InitMappingEngine()
	touchCategoryVersion()
	support.TouchSearchTagsVersion()
	ClearIndexPageCache()
}

// UpdateCategoryStatus 仅更新分类的显示状态或名称，并清除缓存
func UpdateCategoryStatus(id int64, updates map[string]any) error {
	if err := db.Mdb.Model(&model.Category{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return err
	}
	MarkCategoryChanged()
	return nil
}

func flattenCategoryPlacements(nodes []*model.CategoryTree, parentId int64, depth int, out *[]categoryPlacement) error {
	return walkTwoLevelCategoryTree(nodes, parentId, depth, func(item categoryTreeWalkNode) error {
		node := item.Node
		if node == nil || node.Id <= 0 {
			return fmt.Errorf("分类节点数据异常")
		}
		*out = append(*out, categoryPlacement{
			Id:    node.Id,
			Pid:   item.ParentId,
			Sort:  item.Sort,
			Depth: item.Depth,
		})
		return nil
	})
}

func SaveCategoryTreeStructure(nodes []*model.CategoryTree) error {
	placements := make([]categoryPlacement, 0)
	if err := flattenCategoryPlacements(nodes, 0, 0, &placements); err != nil {
		return err
	}

	var categories []model.Category
	if err := db.Mdb.Order("pid ASC, id ASC").Find(&categories).Error; err != nil {
		return err
	}

	oldMap := make(map[int64]model.Category, len(categories))
	nameKeys := make(map[string]int64, len(categories))
	for _, item := range categories {
		oldMap[item.Id] = item
	}
	seen := make(map[int64]struct{}, len(placements))
	for _, placement := range placements {
		item, ok := oldMap[placement.Id]
		if !ok {
			return fmt.Errorf("分类 %d 不存在", placement.Id)
		}
		if _, ok := seen[placement.Id]; ok {
			return fmt.Errorf("分类结构中存在重复节点: %d", placement.Id)
		}
		seen[placement.Id] = struct{}{}
		if placement.Pid != item.Pid {
			return fmt.Errorf("分类 %s 只允许同级排序，不能移动到其他父级", item.Name)
		}
		key := fmt.Sprintf("%d:%s", placement.Pid, strings.TrimSpace(item.Name))
		if exists, ok := nameKeys[key]; ok && exists != placement.Id {
			return fmt.Errorf("同级分类名称重复: %s", item.Name)
		}
		nameKeys[key] = placement.Id
	}
	for id, item := range oldMap {
		if _, ok := seen[id]; ok {
			continue
		}
		return fmt.Errorf("分类 %s 不允许删除，请使用显示/隐藏开关", item.Name)
	}

	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		for _, placement := range placements {
			item := oldMap[placement.Id]
			if placement.Pid == item.Id {
				return fmt.Errorf("分类不能移动到自身下级")
			}
			if err := tx.Model(&model.Category{}).
				Where("id = ?", placement.Id).
				Updates(map[string]any{"pid": placement.Pid, "sort": placement.Sort}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	MarkCategoryChanged()
	return nil
}

// ExistsCategoryTree 查询分类信息是否存在
func ExistsCategoryTree() bool {
	var count int64
	db.Mdb.Table(model.TableCategory).Count(&count)
	return count > 0
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

// InitMainCategories 启动时刷新映射引擎与分类缓存
func InitMainCategories() {
	fmt.Println("[Init] 正在初始化分类表与缓存...")
	ensureCategoryIndexes()
	MarkCategoryChanged()
	fmt.Println("[Init] 分类缓存初始化完成。")
}

func ensureCategoryIndexes() {
	db.Mdb.AutoMigrate(&model.Category{}, &model.CategoryMapping{}, &model.SourceCategory{})
	db.Mdb.Migrator().CreateIndex(&model.Category{}, "uidx_pid_name")
	db.Mdb.Migrator().CreateIndex(&model.CategoryMapping{}, "idx_source_type")
	db.Mdb.Migrator().CreateIndex(&model.CategoryMapping{}, "idx_source_version")
	db.Mdb.Migrator().CreateIndex(&model.SourceCategory{}, "idx_source_parent_sort")
	// 旧库曾把 source_type_id 做成单列唯一索引，多主站常见 type_id 会冲突；启动时重建为复合唯一。
	if db.Mdb.Migrator().HasIndex(&model.SourceCategory{}, "idx_source_type_id") {
		db.Mdb.Migrator().DropIndex(&model.SourceCategory{}, "idx_source_type_id")
	}
	db.Mdb.Migrator().CreateIndex(&model.SourceCategory{}, "idx_source_type_id")
}
