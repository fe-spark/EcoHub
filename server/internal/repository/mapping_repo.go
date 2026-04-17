package repository

import (
	"server/internal/model"
	"server/internal/repository/support"
)

// InitMappingEngine 从数据库加载映射规则并初始化内存缓存
func InitMappingEngine() {
	support.InitMappingEngine()
}

// ReloadMappingRules 强制重新从数据库加载所有映射规则
func ReloadMappingRules() {
	support.ReloadMappingRules()
}

func GetAreaMapping() map[string]string {
	return support.GetAreaMapping()
}

func GetLangMapping() map[string]string {
	return support.GetLangMapping()
}

func GetFilterMap() map[string]bool {
	return support.GetFilterMap()
}

func GetAttributeMapping() map[string]string {
	return support.GetAttributeMapping()
}

func GetPlotMapping() map[string]string {
	return support.GetPlotMapping()
}

func GetMainCategoriesFromCache() []model.Category {
	return support.GetMainCategoriesFromCache()
}

func GetCategoryNameFromCache(id int64) (string, bool) {
	return support.GetCategoryNameFromCache(id)
}

func SetCategoryNameCache(id int64, name string) {
	support.SetCategoryNameCache(id, name)
}
