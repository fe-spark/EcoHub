package support

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"server/internal/infra/db"
	"server/internal/model"
)

func InitMappingEngine() {
	ReloadMappingRules()
}

func ReloadMappingRules() {
	var rules []model.MappingRule
	db.Mdb.Find(&rules)

	if len(rules) == 0 {
		syncInitialRulesToDB()
		db.Mdb.Find(&rules)
	}

	var catMappings []model.CategoryMapping
	db.Mdb.Find(&catMappings)
	newSourceMap := make(map[string]int64)
	for _, m := range catMappings {
		newSourceMap[fmt.Sprintf("%s_%d", m.SourceId, m.SourceTypeId)] = m.CategoryId
	}

	var mains []model.Category
	db.Mdb.Where("pid = 0").Order("id ASC").Find(&mains)
	cacheMainCats = mains

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

	replaceSyncMap(&cacheAreaMap, newArea)
	replaceSyncMap(&cacheLangMap, newLang)
	replaceSyncMapBool(&cacheFilterMap, newFilter)
	replaceSyncMap(&cacheAttribute, newAttr)
	replaceSyncMap(&cachePlotMap, newPlot)
	replaceSyncMapInt64(&cacheSourceMap, newSourceMap)

	RefreshCategoryCache()
}

func syncInitialRulesToDB() {
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

	langs := map[string]string{
		"普通话": "国语", "汉语普通话": "国语", "华语": "国语", "中文字幕": "国语",
		"粤语": "粤语", "韩语": "韩语", "日语": "日语", "英语": "英语",
	}
	for k, v := range langs {
		db.Mdb.FirstOrCreate(&model.MappingRule{Group: "Language", Raw: k, Target: v})
	}

	filters := []string{
		"高清", "蓝光", "1080P", "4K", "HD", "BD", "TS", "TC", "DVD", "VCD",
		"其它", "其他", "全部", "剧情", "暂无", "简介", "正片", "完结", "更新中", "全集", "中字", "字幕",
		"资源", "播放", "线路", "免费", "高速", "极速", "云播", "网盘", "在线",
	}
	for _, f := range filters {
		db.Mdb.FirstOrCreate(&model.MappingRule{Group: "Filter", Raw: f, Target: ""})
	}

	attrs := map[string]string{
		"国产": "大陆", "内地": "大陆", "大陆": "大陆", "香港": "香港", "台湾": "台湾",
		"美国": "美国", "欧美": "美国", "韩国": "韩国", "韩剧": "韩国", "日剧": "日本", "日本": "日本",
	}
	for k, v := range attrs {
		db.Mdb.FirstOrCreate(&model.MappingRule{Group: "Attribute", Raw: k, Target: v})
	}

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

func ResetCategoryNameCache() {
	cacheCategoryMap.Range(func(key, value interface{}) bool {
		cacheCategoryMap.Delete(key)
		return true
	})
}

func GetCategoryBucketRole(typeName string) string {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return model.BigCategoryOther
	}

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
		if !ok {
			continue
		}
		for _, kw := range strings.Split(m.Alias, ",") {
			if kw = strings.TrimSpace(kw); kw != "" && strings.Contains(typeName, kw) {
				return m.Name
			}
		}
	}

	fallbackKeywords := map[string][]string{
		model.BigCategoryAnimation:   {"动漫", "动画", "番剧", "番", "卡通", "漫"},
		model.BigCategoryVariety:     {"综艺", "真人秀", "脱口秀", "晚会"},
		model.BigCategoryDocumentary: {"纪录", "记录", "纪实"},
		model.BigCategoryMovie:       {"电影", "影片", "院线", "影视"},
		model.BigCategoryTV:          {"电视剧", "剧集", "连续剧", "国产剧", "韩剧", "美剧", "日剧", "泰剧", "港剧", "台剧", "短剧"},
	}
	for _, targetName := range matchPriority {
		for _, kw := range fallbackKeywords[targetName] {
			if strings.Contains(typeName, kw) {
				return targetName
			}
		}
	}

	return model.BigCategoryOther
}

func GetLocalCategoryId(sourceId string, sourceTypeId int64) int64 {
	key := fmt.Sprintf("%s_%d", sourceId, sourceTypeId)
	if id, ok := cacheSourceMap.Load(key); ok {
		return id.(int64)
	}
	return 0
}

func GetStandardIdByRole(role string) int64 {
	mains := GetMainCategoriesFromCache()
	for _, m := range mains {
		if m.Name == role {
			return m.Id
		}
	}
	return 0
}

func GetMainCategoryName(pid int64) string {
	if pid <= 0 {
		return ""
	}
	if name, ok := GetCategoryNameFromCache(pid); ok {
		return name
	}

	var m model.Category
	if err := db.Mdb.Where("pid = 0 AND id = ?", pid).First(&m).Error; err == nil {
		SetCategoryNameCache(pid, m.Name)
		return m.Name
	}

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

func NormalizeArea(rawArea string) string {
	if rawArea == "" {
		return ""
	}
	rawArea = regexp.MustCompile(`[/,，、\s\.\+\|]`).ReplaceAllString(rawArea, ",")
	areas := strings.Split(rawArea, ",")
	var result []string
	seen := make(map[string]bool)

	mapping := GetAreaMapping()
	filters := GetFilterMap()

	for _, a := range areas {
		a = strings.TrimSpace(a)
		if a == "" || filters[a] {
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
		if l == "" || filters[l] {
			continue
		}
		if _, isArea := areaMapping[l]; isArea {
			continue
		}
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

func CleanPlotTags(tags string, area string, mainCategory string, category string) string {
	if tags == "" {
		return ""
	}

	filters := GetFilterMap()
	plotMapping := GetPlotMapping()
	tags = regexp.MustCompile(`[/,，、\s\|+]`).ReplaceAllString(tags, ",")
	parts := strings.Split(tags, ",")

	var res []string
	seen := make(map[string]bool)
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || p == category || filters[p] {
			continue
		}
		if mapped, ok := plotMapping[p]; ok {
			p = mapped
		}
		if p != "" && !seen[p] && !filters[p] && p != category && p != mainCategory {
			if len([]rune(p)) <= 4 && len([]rune(p)) >= 2 {
				res = append(res, p)
				seen[p] = true
			}
		}
	}
	return strings.Join(res, ",")
}
