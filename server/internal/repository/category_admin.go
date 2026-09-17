package repository

import (
	"fmt"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

type categoryPlacement struct {
	Id    int64
	Pid   int64
	Sort  int
	Depth int
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

// UpdateCategoryStatus 仅更新分类的显示状态或名称，并清除缓存
func UpdateCategoryStatus(id int64, updates map[string]any) error {
	if err := db.Mdb.Model(&model.Category{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return err
	}
	MarkCategoryChanged()
	return nil
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
