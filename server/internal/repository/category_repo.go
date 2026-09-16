package repository

import (
	"strings"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"
)

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

// ClearCategoryCache 清除分类相关缓存，不触碰已固化的搜索标签。
func ClearCategoryCache() {
	if db.Rdb != nil {
		db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)
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

// ExistsCategoryTree 查询分类信息是否存在
func ExistsCategoryTree() bool {
	var count int64
	db.Mdb.Table(model.TableCategory).Count(&count)
	return count > 0
}
