package repository

import (
	"fmt"
	"regexp"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
)

// GetCategoryBucketRole 根据名称推断其属于哪一个预设标准大类 (精简匹配版)
func GetCategoryBucketRole(typeName string) string {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return model.BigCategoryOther
	}

	// 1. 基于 Alias 词典匹配 (强制匹配优先级，防止“国产动漫”被错误匹配为“电视剧”)
	// 特征明显的分类（动漫、综艺等）应当优先于 电影、电视剧 进行匹配。
	matchPriority := []string{
		model.BigCategoryAnimation,
		model.BigCategoryVariety,
		model.BigCategoryDocumentary,
		model.BigCategoryMovie,
		model.BigCategoryTV,
		model.BigCategoryOther,
	}

	mains := GetMainCategoriesFromCache()
	mainsMap := make(map[string]model.Category)
	for _, m := range mains {
		mainsMap[m.Name] = m
	}

	for _, targetName := range matchPriority {
		m, ok := mainsMap[targetName]
		if !ok || m.Alias == "" {
			continue
		}
		for _, kw := range strings.Split(m.Alias, ",") {
			if kw = strings.TrimSpace(kw); kw != "" && strings.Contains(typeName, kw) {
				return m.Name
			}
		}
	}

	// 2. 语义回退 (Fallback) - 使用切片保证有序匹配，优先匹配特征明显的词
	fallback := []struct {
		Key   string
		Value string
	}{
		{"动漫", model.BigCategoryAnimation},
		{"动画", model.BigCategoryAnimation},
		{"番剧", model.BigCategoryAnimation},
		{"综艺", model.BigCategoryVariety},
		{"娱乐", model.BigCategoryVariety},
		{"纪录", model.BigCategoryDocumentary},
		{"短剧", model.BigCategoryOther},
		{"爽剧", model.BigCategoryOther},
		{"微电影", model.BigCategoryOther},
		{"电影", model.BigCategoryMovie},
		{"片", model.BigCategoryMovie},
		{"院线", model.BigCategoryMovie},
		{"电视剧", model.BigCategoryTV},
		{"剧", model.BigCategoryTV},
		{"国产", model.BigCategoryTV},
	}
	for _, f := range fallback {
		if strings.Contains(typeName, f.Key) {
			return f.Value
		}
	}
	return model.BigCategoryOther
}

// GetLocalCategoryId 根据采集源 ID 和 采集源分类 ID 获取本地分类 ID (方案B: 100% 识别 - 内存缓存优化版)
func GetLocalCategoryId(sourceId string, sourceTypeId int64) int64 {
	key := fmt.Sprintf("%s_%d", sourceId, sourceTypeId)
	if id, ok := cacheSourceMap.Load(key); ok {
		return id.(int64)
	}
	return 0
}

// GetStandardIdByRole 返回标准大类的 ID (动态查库版)
func GetStandardIdByRole(role string) int64 {
	mains := GetMainCategoriesFromCache()
	for _, m := range mains {
		if m.Name == role {
			return m.Id
		}
	}
	return 0
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
	if pid <= 0 {
		return ""
	}

	// 1. 尝试从内存缓存获取
	if name, ok := GetCategoryNameFromCache(pid); ok {
		return name
	}

	// 2. 数据库回溯 (仅当缓存未命中时)
	var m model.Category
	if err := db.Mdb.Where("pid = 0 AND id = ?", pid).First(&m).Error; err == nil {
		SetCategoryNameCache(pid, m.Name)
		return m.Name
	}

	// 3. 兜底回退：如果数据库里也没初始化该 ID，则尝试从内存里捞
	mains := GetMainCategoriesFromCache()
	for _, m := range mains {
		if m.Id == pid {
			return m.Name
		}
	}

	return ""
}

func GetCategoryNameById(id int64) string {
	if id <= 0 {
		return ""
	}
	if name, ok := GetCategoryNameFromCache(id); ok {
		return name
	}

	var c model.Category
	if err := db.Mdb.Where("id = ?", id).First(&c).Error; err == nil {
		SetCategoryNameCache(id, c.Name)
		return c.Name
	}
	return ""
}

// NormalizeArea 标准化地区名称
func NormalizeArea(rawArea string) string {
	if rawArea == "" {
		return ""
	}
	// 多地区处理 (支持全部标准化并保留)
	rawArea = regexp.MustCompile(`[/,，、\s\.\+\|]`).ReplaceAllString(rawArea, ",")
	areas := strings.Split(rawArea, ",")
	var result []string
	seen := make(map[string]bool)

	mapping := GetAreaMapping()
	filters := GetFilterMap()

	for _, a := range areas {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}

		// 过滤不需要的词
		if filters[a] {
			continue
		}

		if mapped, ok := mapping[a]; ok {
			a = mapped
		}
		if a != "" && !seen[a] {
			result = append(result, a)
			seen[a] = true
		}
	}

	if len(result) == 0 {
		return ""
	}
	return strings.Join(result, ",")
}

// NormalizeLanguage 标准化语言名称
func NormalizeLanguage(rawLang string) string {
	if rawLang == "" {
		return ""
	}
	rawLang = regexp.MustCompile(`[/,，、\s]`).ReplaceAllString(rawLang, ",")
	langs := strings.Split(rawLang, ",")
	var result []string
	seen := make(map[string]bool)

	mapping := GetLangMapping()
	areaMapping := GetAreaMapping()
	filters := GetFilterMap()

	for _, l := range langs {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}

		// 过滤
		if filters[l] {
			continue
		}

		// 维度清洗：如果语言名在地区映射里，说明这个词其实是地区，剔除
		if _, isArea := areaMapping[l]; isArea {
			continue
		}
		// 映射
		if mapped, ok := mapping[l]; ok {
			l = mapped
		}
		if l != "" && !seen[l] {
			result = append(result, l)
			seen[l] = true
		}
	}

	if len(result) == 0 {
		return ""
	}
	return strings.Join(result, ",")
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
	if tags == "" {
		return ""
	}

	// 1. 初始化过滤器与规则
	filters := GetFilterMap()
	plotMapping := GetPlotMapping()

	// 2. 预处理分隔符
	tags = regexp.MustCompile(`[/,，、\s\|+]`).ReplaceAllString(tags, ",")
	parts := strings.Split(tags, ",")

	var res []string
	seen := make(map[string]bool)

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || p == category || filters[p] {
			continue
		}

		// A. 映射转换 (如果配置了映射则转换，否则保留原样)
		if mapped, ok := plotMapping[p]; ok {
			p = mapped
		}

		// B. 二次过滤与去重
		if p != "" && !seen[p] && !filters[p] && p != category && p != mainCategory {
			// 排除过于亢长的标签 (非核心剧情)
			if len([]rune(p)) <= 4 && len([]rune(p)) >= 2 {
				res = append(res, p)
				seen[p] = true
			}
		}
	}

	return strings.Join(res, ",")
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
