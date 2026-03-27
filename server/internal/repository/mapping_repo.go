package repository

import (
	"fmt"
	"sync"

	"server/internal/infra/db"
	"server/internal/model"
)

var (
	cacheAreaMap     sync.Map
	cacheLangMap     sync.Map
	cacheFilterMap   sync.Map         // key is the word, value is bool
	cacheAttribute   sync.Map         // key is raw, value is target
	cachePlotMap     sync.Map         // key is raw, value is target
	cacheCategoryMap sync.Map         // key is ID (int64), value is Name (string)
	cacheSourceMap   sync.Map         // key is SourceId_SourceTypeId, value is CategoryId (int64)
	cacheMainCats    []model.Category // 存储顶级大类内存副本，用于名称推断
)

// InitMappingEngine 从数据库加载映射规则并初始化内存缓存
func InitMappingEngine() {
	ReloadMappingRules()
}

// ReloadMappingRules 强制重新从数据库加载所有映射规则
func ReloadMappingRules() {
	// 1. 加载基础 MappingRule
	var rules []model.MappingRule
	db.Mdb.Find(&rules)

	if len(rules) == 0 {
		syncInitialRulesToDB()
		db.Mdb.Find(&rules)
	}

	// 2. 加载 CategoryMapping (方案B 核心缓存)
	var catMappings []model.CategoryMapping
	db.Mdb.Find(&catMappings)
	newSourceMap := make(map[string]int64)
	for _, m := range catMappings {
		newSourceMap[fmt.Sprintf("%s_%d", m.SourceId, m.SourceTypeId)] = m.CategoryId
	}

	// 3. 加载标准大类缓存
	var mains []model.Category
	db.Mdb.Where("pid = 0").Order("id ASC").Find(&mains)
	cacheMainCats = mains

	// 4. 清理并更新其它基础映射
	newArea := make(map[string]string)
	newLang := make(map[string]string)
	newFilter := make(map[string]bool)
	newAttr := make(map[string]string)
	newPlot := make(map[string]string)

	for _, r := range rules {
		switch r.Group {
		case "Area":
			newArea[r.Raw] = r.Target
		case "Language":
			newLang[r.Raw] = r.Target
		case "Filter":
			newFilter[r.Raw] = true
		case "Attribute":
			newAttr[r.Raw] = r.Target
		case "Plot":
			newPlot[r.Raw] = r.Target
		}
	}

	// 批量更新所有内存缓存
	replaceSyncMap(&cacheAreaMap, newArea)
	replaceSyncMap(&cacheLangMap, newLang)
	replaceSyncMapBool(&cacheFilterMap, newFilter)
	replaceSyncMap(&cacheAttribute, newAttr)
	replaceSyncMap(&cachePlotMap, newPlot)
	replaceSyncMapInt64(&cacheSourceMap, newSourceMap)

	// 同时触发旧版分类缓存刷新 (nameToId 等)
	RefreshCategoryCache()
}

// syncInitialRulesToDB 将基础映射同步到数据库
func syncInitialRulesToDB() {
	// 地区映射
	areas := map[string]string{
		"内地": "大陆", "中国": "大陆", "中国大陆": "大陆", "中国内地": "大陆",
		"韩国": "韩国", "南韩": "韩国",
		"日本": "日本",
		"台湾": "台湾", "中国台湾": "台湾",
		"香港": "香港", "中国香港": "香港",
		"美国": "美国", "欧美": "美国",
		"英国": "英国", "泰国": "泰国", "海外": "其他",
	}
	for k, v := range areas {
		db.Mdb.FirstOrCreate(&model.MappingRule{Group: "Area", Raw: k, Target: v})
	}

	// 语言映射
	langs := map[string]string{
		"普通话": "国语", "汉语普通话": "国语", "华语": "国语", "中文字幕": "国语",
		"粤语": "粤语", "韩语": "韩语", "日语": "日语", "英语": "英语",
	}
	for k, v := range langs {
		db.Mdb.FirstOrCreate(&model.MappingRule{Group: "Language", Raw: k, Target: v})
	}

	// 采集常见噪音过滤
	filters := []string{
		"高清", "蓝光", "1080P", "4K", "HD", "BD", "TS", "TC", "DVD", "VCD",
		"其它", "其他", "全部", "剧情", "暂无", "简介", "正片", "完结", "更新中", "全集", "中字", "字幕",
		"资源", "播放", "线路", "免费", "高速", "极速", "云播", "网盘", "在线",
	}
	for _, f := range filters {
		db.Mdb.FirstOrCreate(&model.MappingRule{Group: "Filter", Raw: f, Target: ""})
	}

	// 属性提取映射
	attrs := map[string]string{
		"国产": "大陆", "内地": "大陆", "大陆": "大陆", "香港": "香港", "台湾": "台湾",
		"美国": "美国", "欧美": "美国", "韩国": "韩国", "韩剧": "韩国", "日剧": "日本", "日本": "日本",
	}
	for k, v := range attrs {
		db.Mdb.FirstOrCreate(&model.MappingRule{Group: "Attribute", Raw: k, Target: v})
	}

	// 同时刷新顶级大类缓存 (确保初始 ID 加载)
	var mains []model.Category
	db.Mdb.Where("pid = 0").Find(&mains)
	cacheMainCats = mains
}

func replaceSyncMap(sm *sync.Map, data map[string]string) {
	sm.Range(func(key, value interface{}) bool {
		sm.Delete(key)
		return true
	})
	for k, v := range data {
		sm.Store(k, v)
	}
}

func replaceSyncMapBool(sm *sync.Map, data map[string]bool) {
	sm.Range(func(key, value interface{}) bool {
		sm.Delete(key)
		return true
	})
	for k, v := range data {
		sm.Store(k, v)
	}
}

func replaceSyncMapInt64(sm *sync.Map, data map[string]int64) {
	sm.Range(func(key, value interface{}) bool {
		sm.Delete(key)
		return true
	})
	for k, v := range data {
		sm.Store(k, v)
	}
}

func GetAreaMapping() map[string]string {
	res := make(map[string]string)
	cacheAreaMap.Range(func(k, v interface{}) bool {
		res[k.(string)] = v.(string)
		return true
	})
	return res
}

func GetLangMapping() map[string]string {
	res := make(map[string]string)
	cacheLangMap.Range(func(k, v interface{}) bool {
		res[k.(string)] = v.(string)
		return true
	})
	return res
}

func GetFilterMap() map[string]bool {
	res := make(map[string]bool)
	cacheFilterMap.Range(func(k, v interface{}) bool {
		res[k.(string)] = v.(bool)
		return true
	})
	return res
}

func GetAttributeMapping() map[string]string {
	res := make(map[string]string)
	cacheAttribute.Range(func(k, v interface{}) bool {
		res[k.(string)] = v.(string)
		return true
	})
	return res
}

func GetPlotMapping() map[string]string {
	res := make(map[string]string)
	cachePlotMap.Range(func(k, v interface{}) bool {
		res[k.(string)] = v.(string)
		return true
	})
	return res
}

func GetMainCategoriesFromCache() []model.Category {
	return cacheMainCats
}

func GetCategoryNameFromCache(id int64) (string, bool) {
	val, ok := cacheCategoryMap.Load(id)
	if !ok {
		return "", false
	}
	return val.(string), true
}

func SetCategoryNameCache(id int64, name string) {
	cacheCategoryMap.Store(id, name)
}
