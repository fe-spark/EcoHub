package repository

import (
	"strings"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

func BuildCategoryStableKey(pid int64, name string) string {
	return support.BuildCategoryStableKey(pid, name)
}

func GetCategoryStableKeyByID(id int64) string {
	return support.GetCategoryStableKeyByID(id)
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

// ClearCategoryCache 清除分类相关缓存，不触碰已固化的搜索标签。
func ClearCategoryCache() {
	if db.Rdb != nil {
		db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)
		if keys, err := db.Rdb.Keys(db.Cxt, config.ActiveCategoryTreeKey+":src_*").Result(); err == nil && len(keys) > 0 {
			db.Rdb.Del(db.Cxt, keys...)
		}
	}
	RefreshCategoryCache()
}

func MarkCategoryChanged() {
	ClearCategoryCache()
	InitMappingEngine()
	touchCategoryVersion()
	support.TouchSearchTagsVersion()
	ClearIndexPageCache()
}

// SourceHasCategoryMapping 该采集站是否已经有分类映射。没有映射时入库写不出本地 pid。
func SourceHasCategoryMapping(sourceID string) bool {
	sourceID = strings.TrimSpace(sourceID)
	if db.Mdb == nil || sourceID == "" {
		return false
	}
	var count int64
	if err := db.Mdb.Model(&model.CategoryMapping{}).Where("source_id = ?", sourceID).Limit(1).Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

// SourceIDsWithCategory 返回已经有分类副本的采集站。
func SourceIDsWithCategory(sourceIDs []string) map[string]struct{} {
	ready := make(map[string]struct{})
	if db.Mdb == nil || len(sourceIDs) == 0 {
		return ready
	}
	var found []string
	if err := db.Mdb.Model(&model.CategoryMapping{}).
		Where("source_id IN ?", sourceIDs).
		Distinct().
		Pluck("source_id", &found).Error; err != nil {
		return ready
	}
	for _, id := range found {
		if id != "" {
			ready[id] = struct{}{}
		}
	}
	return ready
}

// ClearSourceCategoryDataTx 清掉一个采集站的分类副本。别的站还在用的展示分类留着。
func ClearSourceCategoryDataTx(tx *gorm.DB, sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if tx == nil || sourceID == "" {
		return nil
	}
	var categoryIDs []int64
	if err := tx.Model(&model.CategoryMapping{}).
		Where("source_id = ? AND category_id > 0", sourceID).
		Pluck("category_id", &categoryIDs).Error; err != nil {
		return err
	}
	if err := tx.Where("source_id = ?", sourceID).Delete(&model.SourceCategory{}).Error; err != nil {
		return err
	}
	if err := tx.Where("source_id = ?", sourceID).Delete(&model.CategoryMapping{}).Error; err != nil {
		return err
	}
	if len(categoryIDs) == 0 {
		return nil
	}
	var stillUsed []int64
	if err := tx.Model(&model.CategoryMapping{}).
		Where("category_id IN ?", categoryIDs).
		Distinct().
		Pluck("category_id", &stillUsed).Error; err != nil {
		return err
	}
	used := make(map[int64]struct{}, len(stillUsed))
	for _, id := range stillUsed {
		used[id] = struct{}{}
	}
	removable := make([]int64, 0, len(categoryIDs))
	seen := make(map[int64]struct{}, len(categoryIDs))
	for _, id := range categoryIDs {
		if _, ok := used[id]; ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		removable = append(removable, id)
	}
	if len(removable) == 0 {
		return nil
	}
	return tx.Where("id IN ?", removable).Delete(&model.Category{}).Error
}

// ExistsCategoryTree 查询分类信息是否存在
func ExistsCategoryTree() bool {
	var count int64
	db.Mdb.Table(model.TableCategory).Count(&count)
	return count > 0
}
