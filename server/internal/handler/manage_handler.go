package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"server/internal/config"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

type ManageHandler struct{}

var ManageHd = new(ManageHandler)

// AdminNotice 后台顶部公告（进入后台即可见）。
type AdminNotice struct {
	Level      string `json:"level"` // error | warning | info
	Code       string `json:"code"`
	Message    string `json:"message"`
	ActionPath string `json:"actionPath,omitempty"`
	ActionText string `json:"actionText,omitempty"`
}

func (h *ManageHandler) ManageIndex(c *gin.Context) {
	dto.Success(gin.H{"notices": []AdminNotice{}}, "后台管理中心", c)
}

func (h *ManageHandler) AppVersion(c *gin.Context) {
	isAdmin := false
	if v, ok := c.Get(config.AuthUserClaims); ok {
		if uc, ok := v.(*utils.UserClaims); ok && uc != nil {
			isAdmin = model.IsAdmin(uc.UserID, uc.Role)
		}
	}
	dto.Success(service.VersionSvc.GetAppVersion(isAdmin), "版本信息获取成功", c)
}

func (h *ManageHandler) UpgradeApp(c *gin.Context) {
	v, ok := c.Get(config.AuthUserClaims)
	if !ok {
		dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "鉴权失败,请重新登录", c)
		return
	}
	uc, ok := v.(*utils.UserClaims)
	if !ok || uc == nil || !model.IsAdmin(uc.UserID, uc.Role) {
		dto.CustomResult(http.StatusForbidden, dto.FAILED, nil, "权限不足，仅超级管理员可执行版本升级", c)
		return
	}
	if err := service.VersionSvc.StartUpgrade(); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("已开始拉取新镜像，容器即将重启", c)
}

// ------------------------------------------------------ 站点基本配置 ------------------------------------------------------

// SiteBasicConfig 网站基本配置
func (h *ManageHandler) SiteBasicConfig(c *gin.Context) {
	cfg, err := service.ManageSvc.GetSiteBasicConfig()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	// 若非超级管理员（如公开未登录接口或访客），清空 ProvideKey 防止敏感密钥泄露
	v, ok := c.Get(config.AuthUserClaims)
	uc, isUser := v.(*utils.UserClaims)
	if !ok || !isUser || uc == nil || !model.IsAdmin(uc.UserID, uc.Role) {
		cfg.ProvideKey = ""
		cfg.Danmaku.AppID = ""
		cfg.Danmaku.AppSecret = ""
	}
	dto.Success(cfg, "网站基本信息获取成功", c)
}

// UpdateSiteBasic 更新网站配置信息
func (h *ManageHandler) UpdateSiteBasic(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		dto.Failed(fmt.Sprint("请求参数异常:  ", err), c)
		return
	}
	var bc model.BasicConfig
	if err := json.Unmarshal(body, &bc); err != nil {
		dto.Failed(fmt.Sprint("请求参数异常:  ", err), c)
		return
	}
	if len(strings.TrimSpace(bc.SiteName)) <= 0 {
		dto.Failed("网站名称不能为空", c)
		return
	}
	var probe struct {
		Danmaku json.RawMessage `json:"danmaku"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		dto.Failed(fmt.Sprint("请求参数异常:  ", err), c)
		return
	}
	if raw := bytes.TrimSpace(probe.Danmaku); len(raw) > 0 && string(raw) != "null" {
		dto.Failed("弹幕配置请使用弹幕接口更新", c)
		return
	}
	if err := service.ManageSvc.UpdateSiteBasic(bc); err != nil {
		dto.Failed(fmt.Sprint("网站配置更新失败:  ", err), c)
		return
	}
	dto.SuccessOnlyMsg("更新成功", c)
}

// SiteAccessConfig 网站访问控制配置
func (h *ManageHandler) SiteAccessConfig(c *gin.Context) {
	cfg, err := service.ManageSvc.GetSiteAccessConfig()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	// 若非超级管理员（如只读访客账号），清空 ProvideKey 防止敏感密钥泄露
	v, ok := c.Get(config.AuthUserClaims)
	uc, isUser := v.(*utils.UserClaims)
	if !ok || !isUser || uc == nil || !model.IsAdmin(uc.UserID, uc.Role) {
		cfg.ProvideKey = ""
	}
	dto.Success(cfg, "访问控制配置获取成功", c)
}

// UpdateSiteAccess 更新网站访问控制配置
func (h *ManageHandler) UpdateSiteAccess(c *gin.Context) {
	var ac model.AccessConfig
	if err := c.ShouldBindJSON(&ac); err != nil {
		dto.Failed(fmt.Sprint("请求参数异常: ", err), c)
		return
	}
	if ac.PrivateAccess && len(strings.TrimSpace(ac.ProvideKey)) <= 0 {
		dto.Failed("开启私有化访问时，必须配置订阅密钥", c)
		return
	}
	if err := service.ManageSvc.UpdateSiteAccessConfig(ac); err != nil {
		dto.Failed(fmt.Sprint("访问控制配置更新失败: ", err), c)
		return
	}
	dto.SuccessOnlyMsg("更新成功", c)
}

// SiteDanmakuConfig 网站弹幕系统配置
func (h *ManageHandler) SiteDanmakuConfig(c *gin.Context) {
	cfg, err := service.ManageSvc.GetSiteDanmakuConfig()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	v, ok := c.Get(config.AuthUserClaims)
	uc, isUser := v.(*utils.UserClaims)
	if !ok || !isUser || uc == nil || !model.IsAdmin(uc.UserID, uc.Role) {
		cfg.AppID = ""
		cfg.AppSecret = ""
	}
	dto.Success(cfg, "弹幕系统配置获取成功", c)
}

// UpdateSiteDanmaku 更新网站弹幕系统配置
func (h *ManageHandler) UpdateSiteDanmaku(c *gin.Context) {
	var cfg model.DanmakuConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		dto.Failed(fmt.Sprint("请求参数异常: ", err), c)
		return
	}
	if err := service.ManageSvc.UpdateSiteDanmakuConfig(cfg); err != nil {
		dto.Failed(fmt.Sprint("弹幕系统配置更新失败: ", err), c)
		return
	}
	dto.SuccessOnlyMsg("更新成功", c)
}

// SiteTipConfig 获取赞赏配置
func (h *ManageHandler) SiteTipConfig(c *gin.Context) {
	tip, err := service.ManageSvc.GetSiteTipConfig()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(tip, "赞赏配置获取成功", c)
}

// UpdateSiteTip 更新赞赏配置
func (h *ManageHandler) UpdateSiteTip(c *gin.Context) {
	var tip model.TipConfig
	if err := c.ShouldBindJSON(&tip); err != nil {
		dto.Failed(fmt.Sprint("请求参数异常: ", err), c)
		return
	}
	if err := service.ManageSvc.UpdateSiteTipConfig(tip); err != nil {
		dto.Failed(fmt.Sprint("赞赏配置更新失败: ", err), c)
		return
	}
	dto.SuccessOnlyMsg("赞赏配置更新成功", c)
}

// SiteNoticeConfig 获取公告配置
func (h *ManageHandler) SiteNoticeConfig(c *gin.Context) {
	notice, err := service.ManageSvc.GetSiteNoticeConfig()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(notice, "公告配置获取成功", c)
}

// UpdateSiteNotice 更新公告配置
func (h *ManageHandler) UpdateSiteNotice(c *gin.Context) {
	var notice model.NoticeConfig
	if err := c.ShouldBindJSON(&notice); err != nil {
		dto.Failed(fmt.Sprint("请求参数异常: ", err), c)
		return
	}
	if err := service.ManageSvc.UpdateSiteNoticeConfig(notice); err != nil {
		dto.Failed(fmt.Sprint("公告配置更新失败: ", err), c)
		return
	}
	dto.SuccessOnlyMsg("公告配置更新成功", c)
}

// ------------------------------------------------------ 配置备份导入/导出 ------------------------------------------------------

// ExportConfigBackup 导出站点配置备份（需管理密码，不含影视库存与账号，仅超级管理员）
func (h *ManageHandler) ExportConfigBackup(c *gin.Context) {
	v, ok := c.Get(config.AuthUserClaims)
	if !ok {
		dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "鉴权失败,请重新登录", c)
		return
	}
	uc, ok := v.(*utils.UserClaims)
	if !ok || uc == nil || !model.IsAdmin(uc.UserID, uc.Role) {
		dto.CustomResult(http.StatusForbidden, dto.FAILED, nil, "权限不足，仅超级管理员可导出配置备份", c)
		return
	}
	var req model.ConfigBackupExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	if strings.TrimSpace(req.Password) == "" || !verifyManagePassword(c, req.Password) {
		dto.Failed("导出失败, 密钥校验失败!!!", c)
		return
	}
	backup, err := service.BackupSvc.ExportConfig()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(backup, "配置备份导出成功", c)
}
// ImportConfigBackup 导入站点配置备份（需管理密码，仅超级管理员）
func (h *ManageHandler) ImportConfigBackup(c *gin.Context) {
	v, ok := c.Get(config.AuthUserClaims)
	if !ok {
		dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "鉴权失败,请重新登录", c)
		return
	}
	uc, ok := v.(*utils.UserClaims)
	if !ok || uc == nil || !model.IsAdmin(uc.UserID, uc.Role) {
		dto.CustomResult(http.StatusForbidden, dto.FAILED, nil, "权限不足，仅超级管理员可导入配置备份", c)
		return
	}
	var req model.ConfigBackupImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	if !verifyManagePassword(c, req.Password) {
		dto.Failed("导入失败, 密钥校验失败!!!", c)
		return
	}
	if err := service.BackupSvc.ImportConfig(req); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("配置备份导入成功", c)
}

func verifyManagePassword(c *gin.Context, password string) bool {
	v, ok := c.Get(config.AuthUserClaims)
	if !ok || v == nil {
		return false
	}
	uc, ok := v.(*utils.UserClaims)
	if !ok || uc == nil {
		return false
	}
	return service.UserSvc.VerifyUserPassword(uc.UserID, password)
}

// ------------------------------------------------------ 映射规则管理 ------------------------------------------------------

// MappingRuleGroups 获取映射规则分组列表
func (h *ManageHandler) MappingRuleGroups(c *gin.Context) {
	dto.Success(service.ManageSvc.ListMappingRuleGroups(), "映射规则分组获取成功", c)
}

// MappingRuleList 获取映射规则分页列表
func (h *ManageHandler) MappingRuleList(c *gin.Context) {
	result, err := service.ManageSvc.ListMappingRules(
		c.DefaultQuery("group", ""),
		c.DefaultQuery("keyword", ""),
		dto.GetPageParams(c),
	)
	if err != nil {
		dto.Failed(fmt.Sprintf("映射规则获取失败: %v", err), c)
		return
	}
	dto.Success(result, "映射规则获取成功", c)
}

// MappingRuleReload 重载映射规则缓存
func (h *ManageHandler) MappingRuleReload(c *gin.Context) {
	service.ManageSvc.ReloadMappingRules()
	dto.SuccessOnlyMsg("映射规则已重载", c)
}

// MappingRuleCheck 检查映射规则冲突
func (h *ManageHandler) MappingRuleCheck(c *gin.Context) {
	var rule model.MappingRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		dto.Failed("映射规则参数异常", c)
		return
	}
	result, err := service.ManageSvc.CheckMappingRuleConflict(rule)
	if err != nil {
		dto.Failed(fmt.Sprintf("映射规则冲突检查失败: %v", err), c)
		return
	}
	dto.Success(result, "映射规则冲突检查成功", c)
}

// MappingRuleSave 保存映射规则（支持新增或更新）
func (h *ManageHandler) MappingRuleSave(c *gin.Context) {
	var rule model.MappingRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		dto.Failed("映射规则参数异常", c)
		return
	}
	if rule.ID > 0 {
		if err := service.ManageSvc.UpdateMappingRule(rule); err != nil {
			dto.Failed(fmt.Sprintf("映射规则更新失败: %v", err), c)
			return
		}
		dto.SuccessOnlyMsg("映射规则更新成功", c)
		return
	}
	if err := service.ManageSvc.CreateMappingRule(rule); err != nil {
		dto.Failed(fmt.Sprintf("映射规则新增失败: %v", err), c)
		return
	}
	dto.SuccessOnlyMsg("映射规则新增成功", c)
}

// MappingRuleDelete 批量删除映射规则
func (h *ManageHandler) MappingRuleDelete(c *gin.Context) {
	var req struct {
		Ids []uint `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("映射规则删除参数异常", c)
		return
	}
	cleanIds := make([]uint, 0, len(req.Ids))
	for _, id := range req.Ids {
		if id > 0 {
			cleanIds = append(cleanIds, id)
		}
	}
	if len(cleanIds) == 0 {
		dto.Failed("缺少需要删除的规则标识", c)
		return
	}
	for _, id := range cleanIds {
		if err := service.ManageSvc.DeleteMappingRule(id); err != nil {
			dto.Failed(fmt.Sprintf("映射规则删除失败: %v", err), c)
			return
		}
	}
	dto.SuccessOnlyMsg("映射规则删除成功", c)
}

// ------------------------------------------------------ 参数校验 ------------------------------------------------------
func validFilmSource(fs model.FilmSource) error {
	if len(fs.Name) <= 0 || len(fs.Name) > 20 {
		return errors.New("资源名称不能为空且长度不能超过20")
	}
	if !utils.ValidURL(fs.Uri) {
		return errors.New("资源链接格式异常, 请输入规范的URL链接")
	}
	if err := utils.ValidateDomainReplaceRules(fs.DomainReplaceRules); err != nil {
		return err
	}
	if fs.Format != "" && fs.Format != model.SourceFormatJSON && fs.Format != model.SourceFormatXML {
		return errors.New("不支持的采集数据格式，仅支持 JSON 或 XML")
	}
	return nil
}
