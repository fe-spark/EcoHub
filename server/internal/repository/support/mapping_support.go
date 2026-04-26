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

	var catMappings []model.CategoryMapping
	db.Mdb.Find(&catMappings)
	newSourceMap := make(map[string]int64)
	for _, m := range catMappings {
		newSourceMap[fmt.Sprintf("%s_%d", m.SourceId, m.SourceTypeId)] = m.CategoryId
	}

	newArea := make(map[string]string)
	newLang := make(map[string]string)
	newFilter := make(map[string]bool)
	newAttr := make(map[string]string)
	newPlot := make(map[string]string)
	newCategoryRoots := make(map[string]string)
	newCategorySubs := make(map[string]string)
	newCategoryRootRegex := make([]categoryRuleMatcher, 0)
	newCategorySubRegex := make([]categoryRuleMatcher, 0)

	for _, r := range rules {
		matchType := normalizeRuleMatchType(r.MatchType)
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
		case "CategoryRoot":
			if matchType == "regex" {
				pattern, err := regexp.Compile(strings.TrimSpace(r.Raw))
				if err != nil {
					continue
				}
				newCategoryRootRegex = append(newCategoryRootRegex, categoryRuleMatcher{Pattern: pattern, Target: r.Target})
				continue
			}
			newCategoryRoots[r.Raw] = r.Target
		case "CategorySub":
			if matchType == "regex" {
				pattern, err := regexp.Compile(strings.TrimSpace(r.Raw))
				if err != nil {
					continue
				}
				newCategorySubRegex = append(newCategorySubRegex, categoryRuleMatcher{Pattern: pattern, Target: r.Target})
				continue
			}
			newCategorySubs[r.Raw] = r.Target
		}
	}

	replaceSyncMap(&cacheAreaMap, newArea)
	replaceSyncMap(&cacheLangMap, newLang)
	replaceSyncMapBool(&cacheFilterMap, newFilter)
	replaceSyncMap(&cacheAttribute, newAttr)
	replaceSyncMap(&cachePlotMap, newPlot)
	replaceSyncMap(&cacheCategoryRootMap, newCategoryRoots)
	replaceSyncMap(&cacheCategorySubMap, newCategorySubs)
	replaceCategoryRegexMatchers(&categoryRootRegexMu, &categoryRootRegex, newCategoryRootRegex)
	replaceCategoryRegexMatchers(&categorySubRegexMu, &categorySubRegex, newCategorySubRegex)
	replaceSyncMapInt64(&cacheSourceMap, newSourceMap)

	RefreshCategoryCache()
}

func replaceCategoryRegexMatchers(mu *sync.RWMutex, target *[]categoryRuleMatcher, data []categoryRuleMatcher) {
	mu.Lock()
	defer mu.Unlock()
	cloned := make([]categoryRuleMatcher, len(data))
	copy(cloned, data)
	*target = cloned
}

func normalizeRuleMatchType(matchType string) string {
	switch strings.TrimSpace(strings.ToLower(matchType)) {
	case "regex":
		return "regex"
	default:
		return "exact"
	}
}

func matchCategoryRegexRule(name string, mu *sync.RWMutex, matchers []categoryRuleMatcher) string {
	mu.RLock()
	defer mu.RUnlock()
	for _, matcher := range matchers {
		if matcher.Pattern == nil || !matcher.Pattern.MatchString(name) {
			continue
		}
		mapped := strings.TrimSpace(matcher.Target)
		if mapped != "" {
			return mapped
		}
		return name
	}
	return ""
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

func GetCategoryRootMapping() map[string]string {
	res := make(map[string]string)
	cacheCategoryRootMap.Range(func(k, v interface{}) bool {
		res[k.(string)] = v.(string)
		return true
	})
	return res
}

func GetCategorySubMapping() map[string]string {
	res := make(map[string]string)
	cacheCategorySubMap.Range(func(k, v interface{}) bool {
		res[k.(string)] = v.(string)
		return true
	})
	return res
}

func NormalizeRootCategoryName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if mapped, ok := GetCategoryRootMapping()[name]; ok && strings.TrimSpace(mapped) != "" {
		return strings.TrimSpace(mapped)
	}
	if mapped := matchCategoryRegexRule(name, &categoryRootRegexMu, categoryRootRegex); mapped != "" {
		return mapped
	}
	return name
}

func NormalizeSubCategoryName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if mapped, ok := GetCategorySubMapping()[name]; ok && strings.TrimSpace(mapped) != "" {
		return strings.TrimSpace(mapped)
	}
	if mapped := matchCategoryRegexRule(name, &categorySubRegexMu, categorySubRegex); mapped != "" {
		return mapped
	}
	return name
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

func GetLocalCategoryId(sourceId string, sourceTypeId int64) int64 {
	key := fmt.Sprintf("%s_%d", sourceId, sourceTypeId)
	if id, ok := cacheSourceMap.Load(key); ok {
		return id.(int64)
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
	rawArea = strings.NewReplacer(
		"制片国家/地区", ",",
		"制片国家地区", ",",
		"制片国家：", ",",
		"制片国家:", ",",
		"制片国家", ",",
		"地区：", ",",
		"地区:", ",",
		"地区", ",",
	).Replace(rawArea)
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
