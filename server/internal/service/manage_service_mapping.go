package service

import (
	"regexp"
	"strings"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
)

type MappingRuleListResult struct {
	List   []model.MappingRule `json:"list"`
	Paging dto.Page            `json:"paging"`
}

type MappingRuleConflictResult struct {
	HasConflict bool                `json:"hasConflict"`
	Rules       []model.MappingRule `json:"rules"`
}

func (s *ManageService) ListMappingRules(group, keyword, sourceID string, paging *dto.Page) (MappingRuleListResult, error) {
	query := repository.MappingRuleQuery{
		Group:    strings.TrimSpace(group),
		Keyword:  strings.TrimSpace(keyword),
		SourceId: strings.TrimSpace(sourceID),
		Paging:   paging,
	}
	allList, err := repository.ListAllMappingRules(query)
	if err != nil {
		return MappingRuleListResult{}, err
	}
	page := resolveCustomMappingRulesPage(paging, len(allList))
	pagedList := sliceCustomMappingRulesPage(allList, page)
	return MappingRuleListResult{
		List:   pagedList,
		Paging: page,
	}, nil
}

func (s *ManageService) ListMappingRuleGroups() []string {
	return repository.ListMappingRuleGroups()
}

func (s *ManageService) ReloadMappingRules() {
	repository.ReloadMappingRules()
}

func (s *ManageService) CreateMappingRule(rule model.MappingRule) error {
	prepareMappingRule(&rule)
	if err := validateMappingRule(rule); err != nil {
		return err
	}
	if err := ensureMappingRuleEffectPointAvailable(rule); err != nil {
		return err
	}
	if err := repository.CreateMappingRule(&rule); err != nil {
		return err
	}
	return refreshProjectedReadModelAfterMappingRuleChange(rule.Group)
}

func (s *ManageService) UpdateMappingRule(rule model.MappingRule) error {
	prepareMappingRule(&rule)
	if rule.ID == 0 {
		return dtoError("规则 ID 不能为空")
	}
	oldRule, err := repository.GetMappingRuleByID(rule.ID)
	if err != nil {
		return err
	}
	if err := validateMappingRule(rule); err != nil {
		return err
	}
	if err := ensureMappingRuleEffectPointAvailable(rule); err != nil {
		return err
	}
	if err := repository.UpdateMappingRule(&rule); err != nil {
		return err
	}
	oldGroup := ""
	if oldRule != nil {
		oldGroup = oldRule.Group
	}
	return refreshProjectedReadModelAfterMappingRuleChange(oldGroup, rule.Group)
}

func (s *ManageService) DeleteMappingRule(id uint) error {
	if id == 0 {
		return dtoError("规则 ID 不能为空")
	}
	rule, err := repository.GetMappingRuleByID(id)
	if err != nil {
		return err
	}
	if err := repository.DeleteMappingRule(id); err != nil {
		return err
	}
	if rule == nil {
		return nil
	}
	return refreshProjectedReadModelAfterMappingRuleChange(rule.Group)
}

func (s *ManageService) CheckMappingRuleConflict(rule model.MappingRule) (MappingRuleConflictResult, error) {
	prepareMappingRule(&rule)
	if rule.Group == "" || rule.Raw == "" {
		return MappingRuleConflictResult{}, nil
	}
	if repository.IsCategoryMappingGroup(rule.Group) && rule.SourceId == "" {
		return MappingRuleConflictResult{}, nil
	}
	conflicts, err := repository.FindMappingRulesByEffectPoint(rule.SourceId, rule.Group, rule.Raw, rule.MatchType, rule.ID)
	if err != nil {
		return MappingRuleConflictResult{}, err
	}
	return MappingRuleConflictResult{
		HasConflict: len(conflicts) > 0,
		Rules:       conflicts,
	}, nil
}

func (s *ManageService) PreviewMappingRule(sourceID, group, raw, matchType, target string) (repository.MappingRulePreviewResult, error) {
	return repository.PreviewMappingRule(sourceID, group, raw, matchType, target)
}

func refreshProjectedReadModelAfterMappingRuleChange(groups ...string) error {
	for _, group := range groups {
		if repository.IsCategoryMappingGroup(group) {
			if err := repository.RefreshFutureCategoryMappingsFromSourceCategories(); err != nil {
				return err
			}
			return filmsnapshot.RefreshActiveProjectedReadModel()
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
	if !repository.IsCategoryMappingGroup(rule.Group) {
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
	for _, group := range repository.ListMappingRuleGroups() {
		allowed[group] = struct{}{}
	}
	if _, ok := allowed[rule.Group]; !ok {
		return dtoError("不支持的规则分组")
	}
	if rule.MatchType != "exact" && rule.MatchType != "regex" {
		return dtoError("不支持的匹配方式")
	}
	if repository.IsCategoryMappingGroup(rule.Group) && rule.SourceId == "" {
		return dtoError("分类规则必须指定采集站")
	}
	if repository.IsCategoryMappingGroup(rule.Group) && rule.MatchType == "regex" {
		if _, err := regexp.Compile(rule.Raw); err != nil {
			return dtoError("正则表达式不合法")
		}
	}
	return nil
}

func ensureMappingRuleEffectPointAvailable(rule model.MappingRule) error {
	conflicts, err := repository.FindMappingRulesByEffectPoint(rule.SourceId, rule.Group, rule.Raw, rule.MatchType, rule.ID)
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
