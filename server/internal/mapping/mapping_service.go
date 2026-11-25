package mapping

import (
	"errors"
	"regexp"
	"strings"

	"server/internal/film"
	"server/internal/film/catalog"
	"server/internal/film/snapshot"
	"server/internal/model"
	"server/internal/model/dto"
)

func dtoError(msg string) error { return errors.New(msg) }

type MappingService struct{}

var MappingSvc = new(MappingService)

type MappingRuleListResult struct {
	List   []model.MappingRule `json:"list"`
	Paging dto.Page            `json:"paging"`
	Totals map[string]int      `json:"totals,omitempty"`
}

func (s *MappingService) ListMappingRules(group, keyword, sourceID string, paging *dto.Page) (MappingRuleListResult, error) {
	query := MappingRuleQuery{
		Group:    strings.TrimSpace(group),
		Keyword:  strings.TrimSpace(keyword),
		SourceId: strings.TrimSpace(sourceID),
		Paging:   paging,
	}
	allList, err := ListAllMappingRules(query)
	if err != nil {
		return MappingRuleListResult{}, err
	}
	var totals map[string]int
	if query.SourceId != "" {
		if (query.Group == "" || query.Group == "CategoryAll") && query.Keyword == "" {
			totals = map[string]int{
				"CategoryRoot": 0,
				"CategorySub":  0,
			}
			for _, item := range allList {
				if item.Group == "CategoryRoot" || item.Group == "CategorySub" {
					totals[item.Group]++
				}
			}
		} else {
			totals, err = CountCategoryMappingRulesByGroup(query.SourceId)
			if err != nil {
				return MappingRuleListResult{}, err
			}
		}
	}
	page := resolveCustomMappingRulesPage(paging, len(allList))
	pagedList := sliceCustomMappingRulesPage(allList, page)
	return MappingRuleListResult{
		List:   pagedList,
		Paging: page,
		Totals: totals,
	}, nil
}

func (s *MappingService) ListMappingRuleGroups() []string {
	return ListMappingRuleGroups()
}

func (s *MappingService) ReloadMappingRules() {
	ReloadMappingRules()
}

func (s *MappingService) CreateMappingRule(rule model.MappingRule) error {
	prepareMappingRule(&rule)
	if err := validateMappingRule(rule); err != nil {
		return err
	}
	if err := ensureMappingRuleEffectPointAvailable(rule); err != nil {
		return err
	}
	if err := CreateMappingRule(&rule); err != nil {
		if IsDuplicateKeyError(err) {
			return dtoError("同分组、同匹配方式、同原始值的规则已存在")
		}
		return err
	}
	return refreshProjectedReadModelAfterMappingRuleChange(rule.Group)
}

func (s *MappingService) UpdateMappingRule(rule model.MappingRule) error {
	prepareMappingRule(&rule)
	if rule.ID == 0 {
		return dtoError("规则 ID 不能为空")
	}
	oldRule, err := GetMappingRuleByID(rule.ID)
	if err != nil {
		return err
	}
	if err := validateMappingRule(rule); err != nil {
		return err
	}
	if err := ensureMappingRuleEffectPointAvailable(rule); err != nil {
		return err
	}
	if err := UpdateMappingRule(&rule); err != nil {
		if IsDuplicateKeyError(err) {
			return dtoError("同分组、同匹配方式、同原始值的规则已存在")
		}
		return err
	}
	oldGroup := ""
	if oldRule != nil {
		oldGroup = oldRule.Group
	}
	return refreshProjectedReadModelAfterMappingRuleChange(oldGroup, rule.Group)
}

func (s *MappingService) DeleteMappingRule(id uint) error {
	return s.DeleteMappingRules([]uint{id})
}

// DeleteMappingRules 批量删除映射规则，统一在全部删除后刷新一次缓存与分类映射。
func (s *MappingService) DeleteMappingRules(ids []uint) error {
	cleanIDs := make([]uint, 0, len(ids))
	seen := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		cleanIDs = append(cleanIDs, id)
	}
	if len(cleanIDs) == 0 {
		return dtoError("规则 ID 不能为空")
	}
	rules, err := GetMappingRulesByIDs(cleanIDs)
	if err != nil {
		return err
	}
	if err := DeleteMappingRules(cleanIDs); err != nil {
		return err
	}
	groups := make([]string, 0, len(rules))
	for _, rule := range rules {
		groups = append(groups, rule.Group)
	}
	return refreshProjectedReadModelAfterMappingRuleChange(groups...)
}

// GetMappingRuleContext 获取指定采集站的原始分类及全部分类映射规则
func (s *MappingService) GetMappingRuleContext(sourceID string) (MappingRuleContextResult, error) {
	return GetMappingRuleContext(sourceID)
}

func refreshProjectedReadModelAfterMappingRuleChange(groups ...string) error {
	for _, group := range groups {
		if IsCategoryMappingGroup(group) {
			if err := catalog.RefreshFutureCategoryMappingsFromSourceCategories(); err != nil {
				return err
			}
			if err := snapshot.RefreshActiveProjectedReadModel(); err != nil {
				return err
			}
			snapshot.ClearDynamicPlayCaches()
			film.RefreshInventoryStatsAsync()
			return nil
		}
	}
	return nil
}

func prepareMappingRule(rule *model.MappingRule) {
	rule.Group = strings.TrimSpace(rule.Group)
	rule.Raw = strings.TrimSpace(rule.Raw)
	rule.Target = strings.TrimSpace(rule.Target)
	rule.MatchType = normalizeMappingRuleMatchType(rule.MatchType)
	rule.Remarks = strings.TrimSpace(rule.Remarks)
	rule.SourceId = strings.TrimSpace(rule.SourceId)
	if !IsCategoryMappingGroup(rule.Group) {
		rule.SourceId = ""
	}
}

func validateMappingRule(rule model.MappingRule) error {
	if rule.Group == "" {
		return dtoError("规则分组不能为空")
	}
	if rule.Raw == "" {
		return dtoError("原始值不能为空")
	}
	allowed := map[string]struct{}{}
	for _, group := range ListMappingRuleGroups() {
		allowed[group] = struct{}{}
	}
	if _, ok := allowed[rule.Group]; !ok {
		return dtoError("不支持的规则分组")
	}
	if rule.MatchType != "exact" && rule.MatchType != "regex" {
		return dtoError("不支持的匹配方式")
	}
	if rule.Group != "Filter" && rule.Target == "" {
		return dtoError("目标值不能为空")
	}
	if !IsCategoryMappingGroup(rule.Group) && rule.MatchType == "regex" {
		return dtoError("当前分组仅支持精确匹配")
	}
	if IsCategoryMappingGroup(rule.Group) && rule.SourceId == "" {
		return dtoError("分类规则必须指定采集站")
	}
	if IsCategoryMappingGroup(rule.Group) && rule.MatchType == "regex" {
		if _, err := regexp.Compile(rule.Raw); err != nil {
			return dtoError("正则表达式不合法")
		}
	}
	return nil
}

func ensureMappingRuleEffectPointAvailable(rule model.MappingRule) error {
	conflicts, err := FindMappingRulesByEffectPoint(rule.SourceId, rule.Group, rule.Raw, rule.MatchType, rule.ID)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return dtoError("同分组、同匹配方式、同原始值的规则已存在")
	}
	return nil
}

func normalizeMappingRuleMatchType(matchType string) string {
	switch strings.TrimSpace(strings.ToLower(matchType)) {
	case "regex":
		return "regex"
	default:
		return "exact"
	}
}

func resolveCustomMappingRulesPage(paging *dto.Page, total int) dto.Page {
	page := dto.Page{Current: 1, PageSize: 20}
	if paging != nil {
		page = *paging
	}
	if page.Current <= 0 {
		page.Current = 1
	}
	if page.PageSize <= 0 {
		page.PageSize = 20
	}
	page.Total = total
	page.PageCount = int((total + page.PageSize - 1) / page.PageSize)
	if page.PageCount <= 0 {
		page.PageCount = 1
	}
	if page.Current > page.PageCount {
		page.Current = page.PageCount
	}
	return page
}

func sliceCustomMappingRulesPage(list []model.MappingRule, page dto.Page) []model.MappingRule {
	if len(list) == 0 {
		return []model.MappingRule{}
	}
	start := (page.Current - 1) * page.PageSize
	if start >= len(list) {
		return []model.MappingRule{}
	}
	end := start + page.PageSize
	if end > len(list) {
		end = len(list)
	}
	return list[start:end]
}
