package repository

import (
	"strings"

	"server/internal/repository/support"
)

// GetCategoryBucketRole 根据名称推断其属于哪一个预设标准大类 (精简匹配版)
func GetCategoryBucketRole(typeName string) string {
	return support.GetCategoryBucketRole(typeName)
}

// GetLocalCategoryId 根据采集源 ID 和 采集源分类 ID 获取本地分类 ID (方案B: 100% 识别 - 内存缓存优化版)
func GetLocalCategoryId(sourceId string, sourceTypeId int64) int64 {
	return support.GetLocalCategoryId(sourceId, sourceTypeId)
}

// GetStandardIdByRole 返回标准大类的 ID (动态查库版)
func GetStandardIdByRole(role string) int64 {
	return support.GetStandardIdByRole(role)
}

// GetMainCategoryIdByName 根据大类名称直接查找本地大类 ID（动态版本）
// 不再依赖硬编码 Alias 正则匹配，直接按名称精确查找内存缓存中的顶级大类。
func GetMainCategoryIdByName(name string, _ int64) int64 {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0
	}
	return GetStandardIdByRole(name)
}

// GetMainCategoryName 根据 ID 获取标准大类名称 (带内存缓存版本)
func GetMainCategoryName(pid int64) string {
	return support.GetMainCategoryName(pid)
}

func GetCategoryNameById(id int64) string {
	return support.GetCategoryNameById(id)
}

// NormalizeArea 标准化地区名称
func NormalizeArea(rawArea string) string {
	return support.NormalizeArea(rawArea)
}

// NormalizeLanguage 标准化语言名称
func NormalizeLanguage(rawLang string) string {
	return support.NormalizeLanguage(rawLang)
}

// MapAttributesFromTypeName 从分类名中提取隐含的属性 (动态配置版)
func MapAttributesFromTypeName(typeName string) (cleanTypeName string, area string) {
	cleanTypeName = typeName

	// 从 mapping_rules 加载属性提取规则
	attrs := GetAttributeMapping()

	for k, v := range attrs {
		if strings.Contains(typeName, k) {
			area = v
			// 移除匹配到的属性词，使分类名更纯粹
			cleanTypeName = strings.ReplaceAll(cleanTypeName, k, "")
			// 进一步对齐剥离后的名称
			cleanTypeName = strings.TrimSuffix(cleanTypeName, "片")
			cleanTypeName = strings.TrimSuffix(cleanTypeName, "剧")
			cleanTypeName = strings.TrimSuffix(cleanTypeName, "资源")
			break
		}
	}

	cleanTypeName = strings.TrimSpace(cleanTypeName)
	if cleanTypeName == "" {
		cleanTypeName = typeName // 无法剥离则保留原样
	}
	return
}

// CleanPlotTags 用于清洗"剧情"标签，纯动态映射版
func CleanPlotTags(tags string, area string, mainCategory string, category string) string {
	return support.CleanPlotTags(tags, area, mainCategory, category)
}

// IsInSlice 检查字符串是否在切片中
func IsInSlice(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
