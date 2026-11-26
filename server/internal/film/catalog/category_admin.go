package catalog

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var categoryOrderWriteMu sync.Mutex

type categoryPlacement struct {
	Id        int64
	Pid       int64
	Sort      int
	Show      bool
	Depth     int
	MemberIds []int64
}

func flattenCategoryPlacements(nodes []*model.CategoryTree, parentId int64, depth int, out *[]categoryPlacement) error {
	return walkTwoLevelCategoryTree(nodes, parentId, depth, func(item categoryTreeWalkNode) error {
		node := item.Node
		if node == nil || node.Id <= 0 {
			return fmt.Errorf("分类节点数据异常")
		}
		*out = append(*out, categoryPlacement{
			Id:        node.Id,
			Pid:       item.ParentId,
			Sort:      item.Sort,
			Show:      node.Show,
			Depth:     item.Depth,
			MemberIds: node.MemberIds,
		})
		return nil
	})
}

func expandSourceOrderPlacements(placements []categoryPlacement, oldMap map[int64]model.Category) []categoryPlacement {
	out := make([]categoryPlacement, 0, len(placements))
	for _, placement := range placements {
		ids := []int64{placement.Id}
		seen := map[int64]struct{}{placement.Id: {}}
		for _, id := range placement.MemberIds {
			if id <= 0 {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		repWasShown := false
		if rep, ok := oldMap[placement.Id]; ok {
			repWasShown = rep.Show
		}
		for _, id := range ids {
			show := placement.Show
			if id != placement.Id && placement.Show == repWasShown {
				if prev, ok := oldMap[id]; ok {
					show = prev.Show
				}
			}
			out = append(out, categoryPlacement{
				Id:    id,
				Pid:   placement.Pid,
				Sort:  placement.Sort,
				Show:  show,
				Depth: placement.Depth,
			})
		}
	}
	return out
}

// SaveSourceCategoryOrder 只改该采集站分类的排序和显示。规则决定层级，这里不写父子关系。
func SaveSourceCategoryOrder(sourceID string, nodes []*model.CategoryTree) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return fmt.Errorf("采集站不能为空")
	}
	placements := make([]categoryPlacement, 0)
	if err := flattenCategoryPlacements(nodes, 0, 0, &placements); err != nil {
		return err
	}

	categoryOrderWriteMu.Lock()
	defer categoryOrderWriteMu.Unlock()

	expectedTree := GetSourceBoundCategoryTree(sourceID)
	expectedIDs := collectTreeNodeAndMemberIDs(expectedTree.Children)

	allowed := sourceMappedCategoryIDs(sourceID)
	if len(allowed) == 0 {
		return fmt.Errorf("该采集站还没有分类")
	}
	ids := make([]int64, 0, len(allowed))
	for id := range allowed {
		ids = append(ids, id)
	}
	var categories []model.Category
	if err := db.Mdb.Where("id IN ?", ids).Find(&categories).Error; err != nil {
		return err
	}
	oldMap := make(map[int64]model.Category, len(categories))
	for _, item := range categories {
		oldMap[item.Id] = item
	}
	placements = expandSourceOrderPlacements(placements, oldMap)

	seen := make(map[int64]struct{}, len(placements))
	for _, placement := range placements {
		if _, ok := allowed[placement.Id]; !ok {
			return fmt.Errorf("分类 %d 不属于该采集站", placement.Id)
		}
		if _, ok := oldMap[placement.Id]; !ok {
			return fmt.Errorf("分类 %d 不存在", placement.Id)
		}
		if _, ok := seen[placement.Id]; ok {
			return fmt.Errorf("分类结构中存在重复节点: %d", placement.Id)
		}
		seen[placement.Id] = struct{}{}
	}
	requiredIDs := expectedIDs
	if len(requiredIDs) == 0 {
		requiredIDs = allowed
	}
	for id := range requiredIDs {
		if _, ok := allowed[id]; !ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		item := oldMap[id]
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = fmt.Sprintf("%d", id)
		}
		return fmt.Errorf("分类 %s 不允许删除，请使用显示/隐藏开关", name)
	}

	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if tx.Migrator().HasTable(&model.FilmSource{}) {
			var src model.FilmSource
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ?", strings.TrimSpace(sourceID)).First(&src).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		for _, placement := range placements {
			if err := tx.Model(&model.Category{}).
				Where("id = ?", placement.Id).
				Updates(map[string]any{"sort": placement.Sort, "show": placement.Show}).Error; err != nil {
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

func collectTreeNodeAndMemberIDs(nodes []*model.CategoryTree) map[int64]struct{} {
	out := make(map[int64]struct{})
	var walk func(list []*model.CategoryTree)
	walk = func(list []*model.CategoryTree) {
		for _, node := range list {
			if node == nil {
				continue
			}
			if node.Id > 0 {
				out[node.Id] = struct{}{}
			}
			for _, id := range node.MemberIds {
				if id > 0 {
					out[id] = struct{}{}
				}
			}
			if len(node.Children) > 0 {
				walk(node.Children)
			}
		}
	}
	walk(nodes)
	return out
}

func sourceMappedCategoryIDs(sourceID string) map[int64]struct{} {
	allowed := make(map[int64]struct{})
	if db.Mdb == nil || sourceID == "" {
		return allowed
	}
	var ids []int64
	if err := db.Mdb.Model(&model.CategoryMapping{}).
		Where("source_id = ? AND category_id > 0", sourceID).
		Pluck("category_id", &ids).Error; err != nil {
		return allowed
	}
	for _, id := range ids {
		if id > 0 {
			allowed[id] = struct{}{}
		}
	}
	var syntheticIDs []int64
	if err := db.Mdb.Model(&model.Category{}).
		Where("stable_key LIKE ?", syntheticRootPrefix(sourceID)+"%").
		Pluck("id", &syntheticIDs).Error; err == nil {
		for _, id := range syntheticIDs {
			if id > 0 {
				allowed[id] = struct{}{}
			}
		}
	}
	return allowed
}

// InitMainCategories 启动时刷新映射引擎与分类缓存
func InitMainCategories() {
	fmt.Println("[Init] 正在初始化分类表与缓存...")
	MarkCategoryChanged()
	fmt.Println("[Init] 分类缓存初始化完成。")
}
