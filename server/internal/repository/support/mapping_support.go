package support

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"server/internal/config"
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

	mappingState.Store(&MappingSnapshot{
		Area:              newArea,
		Lang:              newLang,
		Filter:            newFilter,
		Attribute:         newAttr,
		Plot:              newPlot,
		CategoryRoot:      newCategoryRoots,
		CategorySub:       newCategorySubs,
		CategoryRootRegex: newCategoryRootRegex,
		CategorySubRegex:  newCategorySubRegex,
		Source:            newSourceMap,
	})

	RefreshCategoryCache()
}

func TouchRuleVersion() {
	if db.Rdb == nil {
		return
	}
	db.Rdb.Set(db.Cxt, config.RuleVersionKey, time.Now().UnixNano(), 0)
}

func GetRuleVersion() string {
	if db.Rdb == nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	version, err := db.Rdb.Get(db.Cxt, config.RuleVersionKey).Result()
	if err == nil && version != "" {
		return version
	}
	version = fmt.Sprintf("%d", time.Now().UnixNano())
	db.Rdb.Set(db.Cxt, config.RuleVersionKey, version, 0)
	return version
}

func normalizeRuleMatchType(matchType string) string {
	switch strings.TrimSpace(strings.ToLower(matchType)) {
	case "regex":
		return "regex"
	default:
		return "exact"
	}
}

func GetAreaMapping() map[string]string {
	snap := getMappingSnapshot()
	res := make(map[string]string, len(snap.Area))
	for k, v := range snap.Area {
		res[k] = v
	}
	return res
}

func GetLangMapping() map[string]string {
	snap := getMappingSnapshot()
	res := make(map[string]string, len(snap.Lang))
	for k, v := range snap.Lang {
		res[k] = v
	}
	return res
}

func GetFilterMap() map[string]bool {
	snap := getMappingSnapshot()
	res := make(map[string]bool, len(snap.Filter))
	for k, v := range snap.Filter {
		res[k] = v
	}
	return res
}

func GetAttributeMapping() map[string]string {
	snap := getMappingSnapshot()
	res := make(map[string]string, len(snap.Attribute))
	for k, v := range snap.Attribute {
		res[k] = v
	}
	return res
}

func GetPlotMapping() map[string]string {
	snap := getMappingSnapshot()
	res := make(map[string]string, len(snap.Plot))
	for k, v := range snap.Plot {
		res[k] = v
	}
	return res
}

func GetCategoryRootMapping() map[string]string {
	snap := getMappingSnapshot()
	res := make(map[string]string, len(snap.CategoryRoot))
	for k, v := range snap.CategoryRoot {
		res[k] = v
	}
	return res
}

func GetCategorySubMapping() map[string]string {
	snap := getMappingSnapshot()
	res := make(map[string]string, len(snap.CategorySub))
	for k, v := range snap.CategorySub {
		res[k] = v
	}
	return res
}

func NormalizeRootCategoryName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	snap := getMappingSnapshot()
	if mapped, ok := snap.CategoryRoot[name]; ok && strings.TrimSpace(mapped) != "" {
		return strings.TrimSpace(mapped)
	}
	for _, matcher := range snap.CategoryRootRegex {
		if matcher.Pattern != nil && matcher.Pattern.MatchString(name) {
			if strings.TrimSpace(matcher.Target) != "" {
				return strings.TrimSpace(matcher.Target)
			}
			return name
		}
	}
	return name
}

func NormalizeSubCategoryName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	snap := getMappingSnapshot()
	if mapped, ok := snap.CategorySub[name]; ok && strings.TrimSpace(mapped) != "" {
		return strings.TrimSpace(mapped)
	}
	for _, matcher := range snap.CategorySubRegex {
		if matcher.Pattern != nil && matcher.Pattern.MatchString(name) {
			if strings.TrimSpace(matcher.Target) != "" {
				return strings.TrimSpace(matcher.Target)
			}
			return name
		}
	}
	return name
}

func GetCategoryNameFromCache(id int64) (string, bool) {
	val, ok := categoryNameCache.Load(id)
	if !ok {
		return "", false
	}
	return val.(string), true
}

func SetCategoryNameCache(id int64, name string) {
	categoryNameCache.Store(id, name)
}

func ResetCategoryNameCache() {
	categoryNameCache.Clear()
}

func GetLocalCategoryId(sourceId string, sourceTypeId int64) int64 {
	key := fmt.Sprintf("%s_%d", sourceId, sourceTypeId)
	snap := getMappingSnapshot()
	return snap.Source[key]
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
