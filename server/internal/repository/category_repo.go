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
	filmrepo "server/internal/repository/film"
	"server/internal/repository/support"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func buildCategoryStableKey(pid int64, name string) string {
	return support.BuildCategoryStableKey(pid, name)
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

func normalizeCategoryStableKeys(tx *gorm.DB) error {
	var roots []model.Category
	if err := tx.Where("pid = 0").Order("id ASC").Find(&roots).Error; err != nil {
		return err
	}
	for _, root := range roots {
		rootKey := buildCategoryStableKey(0, root.Name)
		if err := tx.Model(&model.Category{}).Where("id = ?", root.Id).Update("stable_key", rootKey).Error; err != nil {
			return err
		}

		var children []model.Category
		if err := tx.Where("pid = ?", root.Id).Order("id ASC").Find(&children).Error; err != nil {
			return err
		}
		for _, child := range children {
			childKey := fmt.Sprintf("%s/%s", rootKey, strings.TrimSpace(child.Name))
			if err := tx.Model(&model.Category{}).Where("id = ?", child.Id).Update("stable_key", childKey).Error; err != nil {
				return err
			}
		}
	}
	return nil
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

// GetRootIdBySourcePid 通过采集站大类原始 ID 在本地大类列表中按顺序匹配
// 仅用于子分类尚未落库时的兜底，优先级低于 GetRootId(cid)
// SaveCategoryTree 批量保存并同步分类树（方案B: 100% 结构化映射精简版）
func SaveCategoryTree(sourceId string, tree *model.CategoryTree) error {
	if tree == nil {
		return nil
	}

	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		version := time.Now().UnixNano()

		// 2. 遍历采集站大类，建立本地映射
		for _, node := range tree.Children {
			// 推断标准大类名（探测模式）
			standardName := GetCategoryBucketRole(node.Name)
			if standardName == model.BigCategoryOther && len(node.Children) > 0 {
				for _, sub := range node.Children {
					if role := GetCategoryBucketRole(sub.Name); role != model.BigCategoryOther {
						standardName = role
						break
					}
				}
			}

			// 获取或创建本地大类
			var localMain model.Category
			tx.Where("pid = 0 AND name = ?", standardName).FirstOrCreate(&localMain, model.Category{Pid: 0, Name: standardName, StableKey: buildCategoryStableKey(0, standardName), Show: true})
			if localMain.StableKey == "" {
				tx.Model(&model.Category{}).Where("id = ?", localMain.Id).Update("stable_key", buildCategoryStableKey(0, localMain.Name))
			}

			// 3. 记录映射：如果来源大类名称与标准大类不一致，将其作为子分类处理，实现“日本动漫” -> “动漫”下的具体标签
			targetId := localMain.Id
			if node.Name != standardName {
				var localSub model.Category
				tx.Where("pid = ? AND name = ?", localMain.Id, node.Name).FirstOrCreate(&localSub, model.Category{Pid: localMain.Id, Name: node.Name, StableKey: buildCategoryStableKey(localMain.Id, node.Name), Show: true})
				if localSub.StableKey == "" {
					tx.Model(&model.Category{}).Where("id = ?", localSub.Id).Update("stable_key", buildCategoryStableKey(localMain.Id, localSub.Name))
				}
				targetId = localSub.Id
			}
			tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "source_id"}, {Name: "source_type_id"}},
				DoUpdates: clause.Assignments(map[string]any{
					"category_id":     targetId,
					"mapping_version": version,
				}),
			}).Create(&model.CategoryMapping{
				SourceId:       sourceId,
				SourceTypeId:   node.Id,
				CategoryId:     targetId,
				MappingVersion: version,
			})

			// 4. 处理来源子类 (继续挂载到本地标准大类下，平铺结构)
			for _, sub := range node.Children {
				var localSub model.Category
				tx.Where("pid = ? AND name = ?", localMain.Id, sub.Name).FirstOrCreate(&localSub, model.Category{Pid: localMain.Id, Name: sub.Name, StableKey: buildCategoryStableKey(localMain.Id, sub.Name), Show: true})
				if localSub.StableKey == "" {
					tx.Model(&model.Category{}).Where("id = ?", localSub.Id).Update("stable_key", buildCategoryStableKey(localMain.Id, localSub.Name))
				}

				// 记录子类映射 (100% 绑定)
				tx.Clauses(clause.OnConflict{
					Columns: []clause.Column{{Name: "source_id"}, {Name: "source_type_id"}},
					DoUpdates: clause.Assignments(map[string]any{
						"category_id":     localSub.Id,
						"mapping_version": version,
					}),
				}).Create(&model.CategoryMapping{
					SourceId:       sourceId,
					SourceTypeId:   sub.Id,
					CategoryId:     localSub.Id,
					MappingVersion: version,
				})
			}
		}
		tx.Where("source_id = ? AND mapping_version <> ?", sourceId, version).Delete(&model.CategoryMapping{})
		if err := normalizeCategoryStableKeys(tx); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	// 同步完成后刷新内存缓存，确保采集立即可用
	ClearCategoryCache()
	InitMappingEngine()
	touchCategoryVersion()
	ClearIndexPageCache()
	return nil
}

// buildTreeHelper 内部辅助函数：直接从列表构建树形结构内存模型
func buildTreeHelper() model.CategoryTree {
	var allList []model.Category
	db.Mdb.Where("`show` = ?", true).Order("pid ASC, id ASC").Find(&allList)

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
	db.Mdb.Table(model.TableSearchInfo).Distinct("cid").Pluck("cid", &activeCids)
	activeCidMap := make(map[int64]bool)
	for _, id := range activeCids {
		activeCidMap[id] = true
	}

	var activePids []int64
	db.Mdb.Table(model.TableSearchInfo).Distinct("pid").Pluck("pid", &activePids)
	activePidMap := make(map[int64]bool)
	for _, id := range activePids {
		activePidMap[id] = true
	}

	// 3. 构建树
	var allList []model.Category
	db.Mdb.Where("`show` = ?", true).Order("pid ASC, id ASC").Find(&allList)

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
	rootOrder := map[string]int{
		model.BigCategoryMovie:       1,
		model.BigCategoryTV:          2,
		model.BigCategoryAnimation:   3,
		model.BigCategoryVariety:     4,
		model.BigCategoryDocumentary: 5,
		model.BigCategoryOther:       6,
	}
	sort.SliceStable(children, func(i, j int) bool {
		oi, oki := rootOrder[children[i].Name]
		oj, okj := rootOrder[children[j].Name]
		if oki && okj && oi != oj {
			return oi < oj
		}
		if oki != okj {
			return oki
		}
		return children[i].Id < children[j].Id
	})
}

// ClearCategoryCache 清除分类相关的所有缓存 (Redis + 内存映射)
func ClearCategoryCache() {
	db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)
	filmrepo.ClearAllSearchTagsCache()
	RefreshCategoryCache()
}

func MarkCategoryChanged() {
	ClearCategoryCache()
	InitMappingEngine()
	touchCategoryVersion()
	ClearIndexPageCache()
}

// UpdateCategoryStatus 仅更新分类的显示状态或名称，并清除缓存
func UpdateCategoryStatus(id int64, updates map[string]any) error {
	if err := db.Mdb.Model(&model.Category{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return err
	}
	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		return normalizeCategoryStableKeys(tx)
	}); err != nil {
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
	fmt.Println("[Init] 正在确保标准大类并刷新分类缓存...")
	ensureCategoryIndexes()

	// 1. 确保标准大类存在 (电影, 电视剧, 动漫, 综艺, 纪录片, 其他)
	standards := []string{
		model.BigCategoryMovie,
		model.BigCategoryTV,
		model.BigCategoryAnimation,
		model.BigCategoryVariety,
		model.BigCategoryDocumentary,
		model.BigCategoryOther,
	}
	// 设置每个大类的默认匹配正则 (Alias)
	aliases := map[string]string{
		model.BigCategoryAnimation:   "动漫,动画,番剧,日漫,国漫,美漫",
		model.BigCategoryVariety:     "综艺,脱口秀,真人秀,选秀",
		model.BigCategoryDocumentary: "纪录片,历史,文化,自然",
		model.BigCategoryMovie:       "电影,动作,喜剧,爱情,科幻,恐怖,剧情,战争,惊悚",
		model.BigCategoryTV:          "电视剧,国产,美剧,韩剧,日剧,港剧,台剧,泰剧,海外",
		model.BigCategoryOther:       "其他,其它,解说,福利,短剧,爽剧,微电影",
	}

	for _, name := range standards {
		// 1. 先查找是否存在该记录
		var cat model.Category
		err := db.Mdb.Model(&model.Category{}).Where("pid = 0 AND name = ?", name).First(&cat).Error

		if err == nil {
			// 2. 存在则更新展示状态与别名
			cat.StableKey = buildCategoryStableKey(0, name)
			cat.Alias = aliases[name]
			cat.Show = true
			if saveErr := db.Mdb.Save(&cat).Error; saveErr != nil {
				fmt.Printf("[Error] 更新大类 %s 失败: %v\n", name, saveErr)
			} else {
				fmt.Printf("[Init] 已对齐标准大类: %s (ID: %d)\n", name, cat.Id)
			}
		} else {
			// 3. 不存在则创建
			db.Mdb.Create(&model.Category{
				Pid:       0,
				Name:      name,
				StableKey: buildCategoryStableKey(0, name),
				Alias:     aliases[name],
				Show:      true,
			})
			fmt.Printf("[Init] 已创建标准大类: %s\n", name)
		}
	}
	mergeShortFilmIntoOther()

	// 2. 刷新映射引擎（加载顶级大类到内存缓存）
	InitMappingEngine()

	// 3. 统一清理分类相关缓存并重建内存映射
	_ = db.Mdb.Transaction(func(tx *gorm.DB) error {
		return normalizeCategoryStableKeys(tx)
	})
	MarkCategoryChanged()

	fmt.Println("[Init] 缓存刷新与标准大类对齐完成。")
}

func mergeShortFilmIntoOther() {
	var other model.Category
	if err := db.Mdb.Where("pid = 0 AND name = ?", model.BigCategoryOther).First(&other).Error; err != nil {
		return
	}
	var short model.Category
	if err := db.Mdb.Where("pid = 0 AND name = ?", model.BigCategoryShortFilm).First(&short).Error; err != nil {
		return
	}

	_ = db.Mdb.Transaction(func(tx *gorm.DB) error {
		tx.Model(&model.Category{}).Where("id = ?", short.Id).Update("show", false)

		var shortChildren []model.Category
		tx.Where("pid = ?", short.Id).Find(&shortChildren)

		idMap := make(map[int64]int64)
		for _, child := range shortChildren {
			var target model.Category
			if err := tx.Where("pid = ? AND name = ?", other.Id, child.Name).
				FirstOrCreate(&target, model.Category{Pid: other.Id, Name: child.Name, StableKey: buildCategoryStableKey(other.Id, child.Name), Show: true}).Error; err == nil {
				idMap[child.Id] = target.Id
			}
		}

		tx.Table(model.TableSearchInfo).Where("pid = ?", short.Id).Update("pid", other.Id)
		tx.Table(model.TableSearchInfo).Where("cid = ?", short.Id).Update("cid", other.Id)
		tx.Model(&model.CategoryMapping{}).Where("category_id = ?", short.Id).Update("category_id", other.Id)

		for oldID, newID := range idMap {
			tx.Table(model.TableSearchInfo).Where("cid = ?", oldID).Update("cid", newID)
			tx.Model(&model.CategoryMapping{}).Where("category_id = ?", oldID).Update("category_id", newID)
			tx.Model(&model.Category{}).Where("id = ?", oldID).Update("show", false)
		}

		tx.Where("pid = ? AND tag_type = ?", short.Id, "Category").Delete(&model.SearchTagItem{})
		tx.Where("pid = ? AND tag_type = ?", other.Id, "Category").Delete(&model.SearchTagItem{})
		tx.Exec(
			"INSERT INTO "+model.TableSearchTag+" (created_at,updated_at,pid,tag_type,name,value,score) "+
				"SELECT NOW(),NOW(),s.pid,'Category',c.name,CAST(s.cid AS CHAR),COUNT(*) "+
				"FROM "+model.TableSearchInfo+" s "+
				"JOIN "+model.TableCategory+" c ON c.id = s.cid "+
				"WHERE s.pid = ? AND s.cid > 0 AND s.cid <> s.pid "+
				"GROUP BY s.pid,s.cid,c.name", other.Id,
		)
		if err := normalizeCategoryStableKeys(tx); err != nil {
			return err
		}
		return nil
	})
	MarkCategoryChanged()
}

func ensureCategoryIndexes() {
	db.Mdb.AutoMigrate(&model.Category{}, &model.CategoryMapping{})
	db.Mdb.Migrator().CreateIndex(&model.Category{}, "uidx_pid_name")
	db.Mdb.Migrator().CreateIndex(&model.CategoryMapping{}, "idx_source_type")
	db.Mdb.Migrator().CreateIndex(&model.CategoryMapping{}, "idx_source_version")
}
