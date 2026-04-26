package service

import (
	"regexp"
	"strings"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
)

type ManageService struct{}

// NewManageService 创建管理服务实例
func NewManageService() *ManageService {
	return &ManageService{}
}

var ManageSvc = new(ManageService)

// GetSiteBasicConfig 获取网站基本配置信息
func (s *ManageService) GetSiteBasicConfig() model.BasicConfig {
	return repository.GetSiteBasic()
}

// UpdateSiteBasic 更新网站配置信息
func (s *ManageService) UpdateSiteBasic(c model.BasicConfig) error {
	return repository.SaveSiteBasic(c)
}

// ResetSiteBasic 重置网站配置信息
func (s *ManageService) ResetSiteBasic() error {
	return repository.SaveSiteBasic(defaultBasicConfig())
}

// GetBanners 获取轮播组件信息
func (s *ManageService) GetBanners() model.Banners {
	return repository.GetBanners()
}

// SaveBanners 保存轮播信息
func (s *ManageService) SaveBanners(bl model.Banners) error {
	return repository.SaveBanners(bl)
}

type MappingRuleListResult struct {
	List   []model.MappingRule `json:"list"`
	Paging dto.Page            `json:"paging"`
}

type MappingRuleConflictResult struct {
	HasConflict bool                `json:"hasConflict"`
	Rules       []model.MappingRule `json:"rules"`
}

func (s *ManageService) ListMappingRules(group, keyword string, paging *dto.Page) (MappingRuleListResult, error) {
	query := repository.MappingRuleQuery{
		Group:   strings.TrimSpace(group),
		Keyword: strings.TrimSpace(keyword),
		Paging:  paging,
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
	rule.Group = strings.TrimSpace(rule.Group)
	rule.Raw = strings.TrimSpace(rule.Raw)
	rule.Target = strings.TrimSpace(rule.Target)
	rule.MatchType = normalizeMappingRuleMatchType(rule.MatchType)
	rule.Remarks = strings.TrimSpace(rule.Remarks)
	if err := validateMappingRule(rule); err != nil {
		return err
	}
	if err := ensureMappingRuleEffectPointAvailable(rule); err != nil {
		return err
	}
	return repository.CreateMappingRule(&rule)
}

func (s *ManageService) UpdateMappingRule(rule model.MappingRule) error {
	rule.Group = strings.TrimSpace(rule.Group)
	rule.Raw = strings.TrimSpace(rule.Raw)
	rule.Target = strings.TrimSpace(rule.Target)
	rule.MatchType = normalizeMappingRuleMatchType(rule.MatchType)
	rule.Remarks = strings.TrimSpace(rule.Remarks)
	if rule.ID == 0 {
		return dtoError("规则 ID 不能为空")
	}
	if err := validateMappingRule(rule); err != nil {
		return err
	}
	if err := ensureMappingRuleEffectPointAvailable(rule); err != nil {
		return err
	}
	return repository.UpdateMappingRule(&rule)
}

func (s *ManageService) DeleteMappingRule(id uint) error {
	if id == 0 {
		return dtoError("规则 ID 不能为空")
	}
	return repository.DeleteMappingRule(id)
}

func (s *ManageService) CheckMappingRuleConflict(rule model.MappingRule) (MappingRuleConflictResult, error) {
	rule.Group = strings.TrimSpace(rule.Group)
	rule.Raw = strings.TrimSpace(rule.Raw)
	rule.MatchType = normalizeMappingRuleMatchType(rule.MatchType)
	if rule.Group == "" || rule.Raw == "" {
		return MappingRuleConflictResult{}, nil
	}
	conflicts, err := repository.FindMappingRulesByEffectPoint(rule.Group, rule.Raw, rule.MatchType, rule.ID)
	if err != nil {
		return MappingRuleConflictResult{}, err
	}
	return MappingRuleConflictResult{
		HasConflict: len(conflicts) > 0,
		Rules:       conflicts,
	}, nil
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
	if repository.IsCategoryMappingGroup(rule.Group) && rule.MatchType == "regex" {
		if _, err := regexp.Compile(rule.Raw); err != nil {
			return dtoError("正则表达式不合法")
		}
	}
	return nil
}

func ensureMappingRuleEffectPointAvailable(rule model.MappingRule) error {
	conflicts, err := repository.FindMappingRulesByEffectPoint(rule.Group, rule.Raw, rule.MatchType, rule.ID)
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

func dtoError(msg string) error {
	return &manageError{message: msg}
}

type manageError struct {
	message string
}

func (e *manageError) Error() string {
	return e.message
}
