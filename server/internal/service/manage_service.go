package service

import (
	"regexp"
	"strings"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
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

// UpdateSiteBasic 更新网站基本信息（保留已存的 Tip 和 Notice，除非显式传入）
func (s *ManageService) UpdateSiteBasic(bc model.BasicConfig) error {
	curr := repository.GetSiteBasic()
	curr.SiteName = bc.SiteName
	curr.SiteURL = bc.SiteURL
	curr.Logo = bc.Logo
	curr.Keyword = bc.Keyword
	curr.Describe = bc.Describe
	curr.State = bc.State
	curr.Hint = bc.Hint
	if bc.Tip.Title != "" || len(bc.Tip.Channels) > 0 {
		curr.Tip = bc.Tip
	}
	if bc.Notice.Title != "" || bc.Notice.Content != "" {
		curr.Notice = bc.Notice
	}
	return repository.SaveSiteBasic(curr)
}

// GetSiteTipConfig 获取赞赏配置
func (s *ManageService) GetSiteTipConfig() model.TipConfig {
	return repository.GetSiteBasic().Tip
}

// UpdateSiteTipConfig 更新赞赏配置
func (s *ManageService) UpdateSiteTipConfig(tip model.TipConfig) error {
	curr := repository.GetSiteBasic()
	curr.Tip = tip
	return repository.SaveSiteBasic(curr)
}

// GetSiteNoticeConfig 获取站点公告配置
func (s *ManageService) GetSiteNoticeConfig() model.NoticeConfig {
	return repository.GetSiteBasic().Notice
}

// UpdateSiteNoticeConfig 更新站点公告配置
func (s *ManageService) UpdateSiteNoticeConfig(notice model.NoticeConfig) error {
	curr := repository.GetSiteBasic()
	curr.Notice = notice
	return repository.SaveSiteBasic(curr)
}

// GetBanners 获取轮播组件信息（实时叠加片库状态与海报源）
func (s *ManageService) GetBanners() model.Banners {
	return OverlayBannerLiveRemarks(repository.GetBanners())
}

// SaveBanners 保存轮播信息
func (s *ManageService) SaveBanners(bl model.Banners) error {
	if len(bl) == 0 {
		return repository.SaveBanners(bl)
	}
	// 若轮播项为跟随海报源（IsCustomPic == false），自动查询快照获取最新海报与幻灯图打底并清空 custom_picture
	mids := make([]int64, 0, len(bl))
	for _, b := range bl {
		if b.Mid > 0 && !b.IsCustomPic {
			mids = append(mids, b.Mid)
		}
	}
	if len(mids) > 0 {
		liveData := filmrepo.LiveBannerSnapshotsByMIDs(mids)
		for i := range bl {
			if !bl[i].IsCustomPic {
				bl[i].CustomPicture = ""
				if snap, ok := liveData[bl[i].Mid]; ok {
					dispPic := snap.DisplayPicture()
					if dispPic != "" {
						bl[i].Picture = dispPic
						bl[i].Poster = dispPic
					}
					dispSlide := snap.DisplayPictureSlide()
					if dispSlide != "" {
						bl[i].PictureSlide = dispSlide
					} else if dispPic != "" {
						bl[i].PictureSlide = dispPic
					}
				}
			}
		}
	}
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
	if err := repository.CreateMappingRule(&rule); err != nil {
		return err
	}
	return refreshProjectedReadModelAfterMappingRuleChange(rule.Group)
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

func refreshProjectedReadModelAfterMappingRuleChange(groups ...string) error {
	for _, group := range groups {
		if repository.IsCategoryMappingGroup(group) {
			if err := repository.RefreshFutureCategoryMappingsFromSourceCategories(); err != nil {
				return err
			}
			return filmrepo.RefreshActiveProjectedReadModel()
		}
	}
	return nil
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
