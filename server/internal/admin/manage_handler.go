package admin

import (
	"server/internal/mediaproxy"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"server/internal/config"
	"server/internal/httpx"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/portal"
	"server/internal/user"
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
	dto.Success(VersionSvc.GetAppVersion(httpx.IsAdminRequest(c)), "版本信息获取成功", c)
}

func (h *ManageHandler) UpgradeApp(c *gin.Context) {
	if _, ok := httpx.RequireAdminClaims(c, "权限不足，仅超级管理员可执行版本升级"); !ok {
		return
	}
	if err := VersionSvc.StartUpgrade(); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("已开始拉取新镜像，容器即将重启", c)
}

// ------------------------------------------------------ 站点基本配置 ------------------------------------------------------

// SiteBasicConfig 网站基本配置
func (h *ManageHandler) SiteBasicConfig(c *gin.Context) {
	cfg, err := ManageSvc.GetSiteBasicConfig()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	if !httpx.IsAdminRequest(c) {
		cfg.ProvideKey = ""
		cfg.Danmaku.AppID = ""
		cfg.Danmaku.AppSecret = ""
		cfg.Danmaku.ApiURL = ""
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
	if err := ManageSvc.UpdateSiteBasic(bc); err != nil {
		dto.Failed(fmt.Sprint("网站配置更新失败:  ", err), c)
		return
	}
	dto.SuccessOnlyMsg("更新成功", c)
}

// SiteAccessConfig 网站访问控制配置
func (h *ManageHandler) SiteAccessConfig(c *gin.Context) {
	cfg, err := ManageSvc.GetSiteAccessConfig()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	if !httpx.IsAdminRequest(c) {
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
	if err := ManageSvc.UpdateSiteAccessConfig(ac); err != nil {
		dto.Failed(fmt.Sprint("访问控制配置更新失败: ", err), c)
		return
	}
	dto.SuccessOnlyMsg("更新成功", c)
}

// SiteMediaProxyConfig 网站媒体图片边缘反代配置
func (h *ManageHandler) SiteMediaProxyConfig(c *gin.Context) {
	cfg, err := ManageSvc.GetSiteMediaProxyConfig()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(cfg, "媒体反代配置获取成功", c)
}

// UpdateSiteMediaProxy 更新网站媒体图片边缘反代配置
func (h *ManageHandler) UpdateSiteMediaProxy(c *gin.Context) {
	var cfg model.MediaProxyConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		dto.Failed(fmt.Sprint("请求参数异常: ", err), c)
		return
	}
	cfg.Template = strings.TrimSpace(cfg.Template)
	if err := mediaproxy.ValidateMediaProxyConfig(cfg); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	if err := ManageSvc.UpdateSiteMediaProxyConfig(cfg); err != nil {
		dto.Failed(fmt.Sprint("媒体反代配置更新失败: ", err), c)
		return
	}
	dto.Success(cfg, "媒体反代配置更新成功", c)
}

// SiteDanmakuConfig 网站弹幕系统配置
func (h *ManageHandler) SiteDanmakuConfig(c *gin.Context) {
	cfg, err := ManageSvc.GetSiteDanmakuConfig()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	if !httpx.IsAdminRequest(c) {
		cfg.AppID = ""
		cfg.AppSecret = ""
		cfg.ApiURL = ""
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
	if strings.TrimSpace(cfg.ApiURL) != "" && !utils.IsSafeOutboundURL(cfg.ApiURL) {
		dto.Failed("弹幕接口地址无效或禁止访问内网地址", c)
		return
	}
	if err := ManageSvc.UpdateSiteDanmakuConfig(cfg); err != nil {
		dto.Failed(fmt.Sprint("弹幕系统配置更新失败: ", err), c)
		return
	}
	dto.SuccessOnlyMsg("更新成功", c)
}

// TestSiteDanmaku 用后台填写的弹幕配置（可未保存）试拉一次全网弹幕，不读写缓存。
// AppId / AppSecret 留空时沿用已保存的值，和保存接口一致。
func (h *ManageHandler) TestSiteDanmaku(c *gin.Context) {
	var req struct {
		Config  model.DanmakuConfig `json:"config"`
		Title   string              `json:"title"`
		Episode int                 `json:"episode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed(fmt.Sprint("请求参数异常: ", err), c)
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		dto.Failed("请填写用来测试的片名", c)
		return
	}
	if req.Episode <= 0 {
		req.Episode = 1
	}
	cfg := req.Config
	if strings.TrimSpace(cfg.ApiURL) != "" && !utils.IsSafeOutboundURL(cfg.ApiURL) {
		dto.Failed("弹幕接口地址无效或禁止访问内网地址", c)
		return
	}
	if saved, err := ManageSvc.GetSiteDanmakuConfig(); err == nil {
		if strings.TrimSpace(cfg.AppID) == "" {
			cfg.AppID = saved.AppID
		}
		if strings.TrimSpace(cfg.AppSecret) == "" {
			cfg.AppSecret = saved.AppSecret
		}
	}
	res := portal.FetchSniffedDanmaku(cfg, title, req.Episode, false)
	dto.Success(gin.H{
		"status":       res.Status,
		"count":        len(res.Items),
		"reason":       res.Reason,
		"animeTitle":   res.AnimeTitle,
		"episodeTitle": res.EpisodeTitle,
	}, "测试完成", c)
}

// SiteTipConfig 获取赞赏配置
func (h *ManageHandler) SiteTipConfig(c *gin.Context) {
	tip, err := ManageSvc.GetSiteTipConfig()
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
	if err := ManageSvc.UpdateSiteTipConfig(tip); err != nil {
		dto.Failed(fmt.Sprint("赞赏配置更新失败: ", err), c)
		return
	}
	dto.SuccessOnlyMsg("赞赏配置更新成功", c)
}

// SiteNoticeConfig 获取公告配置
func (h *ManageHandler) SiteNoticeConfig(c *gin.Context) {
	notice, err := ManageSvc.GetSiteNoticeConfig()
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
	if err := ManageSvc.UpdateSiteNoticeConfig(notice); err != nil {
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
	backup, err := BackupSvc.ExportConfig()
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
	if err := BackupSvc.ImportConfig(req); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("配置备份导入成功", c)
}

func verifyManagePassword(c *gin.Context, password string) bool {
	return httpx.VerifyCurrentPassword(c, password, user.UserSvc.VerifyUserPassword)
}
