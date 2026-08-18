package handler

import (
	"errors"
	"fmt"
	"strconv"
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
	// notices 预留后台顶部公告；ContentKey 已改为写路径懒兼容，不再在启动时 bulk 迁移。
	dto.Success(gin.H{"notices": []AdminNotice{}}, "后台管理中心", c)
}

func (h *ManageHandler) AppVersion(c *gin.Context) {
	dto.Success(service.VersionSvc.GetAppVersion(), "版本信息获取成功", c)
}

func (h *ManageHandler) UpgradeApp(c *gin.Context) {
	if err := service.VersionSvc.StartUpgrade(); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("已开始拉取 latest，容器即将重启", c)
}

// ------------------------------------------------------ 站点基本配置 ------------------------------------------------------

// SiteBasicConfig  网站基本配置
func (h *ManageHandler) SiteBasicConfig(c *gin.Context) {
	dto.Success(service.ManageSvc.GetSiteBasicConfig(), "网站基本信息获取成功", c)
}

// UpdateSiteBasic 更新网站配置信息
func (h *ManageHandler) UpdateSiteBasic(c *gin.Context) {
	bc := model.BasicConfig{}
	if err := c.ShouldBindJSON(&bc); err == nil {
		if len(bc.SiteName) <= 0 {
			dto.Failed("网站名称不能为空", c)
			return
		}
	} else {
		dto.Failed(fmt.Sprint("请求参数异常:  ", err), c)
		return
	}

	if err := service.ManageSvc.UpdateSiteBasic(bc); err != nil {
		dto.Failed(fmt.Sprint("网站配置更新失败:  ", err), c)
		return
	}
	dto.SuccessOnlyMsg("更新成功", c)
}

// ResetSiteBasic 重置网站基本信息为默认值（首页轮播已移入内容管理，重置不再清空轮播）
func (h *ManageHandler) ResetSiteBasic(c *gin.Context) {
	if err := service.ManageSvc.ResetSiteBasic(); err != nil {
		dto.Failed(fmt.Sprint("配置信息重置失败: ", err), c)
		return
	}
	dto.SuccessOnlyMsg("已还原默认网站配置", c)
}

// ------------------------------------------------------ 配置备份导入/导出 ------------------------------------------------------

// ExportConfigBackup 导出站点配置备份（不含影视库存与账号）
func (h *ManageHandler) ExportConfigBackup(c *gin.Context) {
	dto.Success(service.BackupSvc.ExportConfig(), "配置备份导出成功", c)
}

// ImportConfigBackup 导入站点配置备份（需管理密码）
func (h *ManageHandler) ImportConfigBackup(c *gin.Context) {
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
	if !ok {
		dto.Failed("操作失败,登录信息异常!!!", c)
		return false
	}
	uc := v.(*utils.UserClaims)
	return service.UserSvc.VerifyUserPassword(uc.UserID, password)
}

// ------------------------------------------------------ 轮播数据配置 ------------------------------------------------------

// BannerList 获取轮播图数据
func (h *ManageHandler) BannerList(c *gin.Context) {
	bl := service.ManageSvc.GetBanners()
	dto.Success(bl, "配置信息获取成功", c)
}

// BannerFind 返回ID对应的横幅信息
func (h *ManageHandler) BannerFind(c *gin.Context) {
	id := c.Query("id")
	if id == "" {
		dto.Failed("Banner信息获取失败, ID信息异常", c)
		return
	}
	bl := service.ManageSvc.GetBanners()
	for _, b := range bl {
		if b.Id == id {
			dto.Success(b, "Banner信息获取成功", c)
			return
		}
	}
	dto.Failed("Banner信息获取失败", c)
}

// BannerAdd  添加海报数据
func (h *ManageHandler) BannerAdd(c *gin.Context) {
	var b model.Banner
	if err := c.ShouldBindJSON(&b); err != nil {
		dto.Failed("Banner参数提交异常", c)
		return
	}
	b.Id = utils.GenerateSalt()
	bl := service.ManageSvc.GetBanners()
	if len(bl) >= 6 {
		dto.Failed("首页轮播最多支持 6 条，无法继续添加", c)
		return
	}
	bl = append(bl, b)
	if err := service.ManageSvc.SaveBanners(bl); err != nil {
		dto.Failed(fmt.Sprintln("Banners信息添加失败,", err), c)
		return
	}
	dto.SuccessOnlyMsg("海报信息添加成功", c)
}

// BannerUpdate  更新海报数据
func (h *ManageHandler) BannerUpdate(c *gin.Context) {
	var banner model.Banner
	if err := c.ShouldBindJSON(&banner); err != nil {
		dto.Failed("Banner参数提交异常", c)
		return
	}
	bl := service.ManageSvc.GetBanners()
	for i, b := range bl {
		if b.Id == banner.Id {
			bl[i] = banner
			if err := service.ManageSvc.SaveBanners(bl); err != nil {
				dto.Failed("海报信息更新失败", c)
			} else {
				dto.SuccessOnlyMsg("海报信息更新成功", c)
				return
			}
		}
	}
	dto.Failed("海报信息更新失败, 未匹配对应Banner信息", c)
}

// BannerDel 删除海报数据
func (h *ManageHandler) BannerDel(c *gin.Context) {
	var req struct {
		Id string `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("Banner信息获取失败, 请求参数异常", c)
		return
	}
	id := strings.TrimSpace(req.Id)
	if id == "" {
		dto.Failed("Banner信息获取失败, ID信息异常", c)
		return
	}
	bl := service.ManageSvc.GetBanners()
	for i, b := range bl {
		if b.Id == id {
			bl = append(bl[:i], bl[i+1:]...)
			_ = service.ManageSvc.SaveBanners(bl)
			dto.SuccessOnlyMsg("海报信息删除成功", c)
			return
		}
	}
	dto.Failed("海报信息删除失败", c)
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

// MappingRuleAdd 新增映射规则
func (h *ManageHandler) MappingRuleAdd(c *gin.Context) {
	var rule model.MappingRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		dto.Failed("映射规则参数异常", c)
		return
	}
	if err := service.ManageSvc.CreateMappingRule(rule); err != nil {
		dto.Failed(fmt.Sprintf("映射规则新增失败: %v", err), c)
		return
	}
	dto.SuccessOnlyMsg("映射规则新增成功", c)
}

// MappingRuleUpdate 更新映射规则
func (h *ManageHandler) MappingRuleUpdate(c *gin.Context) {
	var rule model.MappingRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		dto.Failed("映射规则参数异常", c)
		return
	}
	if err := service.ManageSvc.UpdateMappingRule(rule); err != nil {
		dto.Failed(fmt.Sprintf("映射规则更新失败: %v", err), c)
		return
	}
	dto.SuccessOnlyMsg("映射规则更新成功", c)
}

// MappingRuleDel 删除映射规则
func (h *ManageHandler) MappingRuleDel(c *gin.Context) {
	var req struct {
		ID uint `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("映射规则删除参数异常", c)
		return
	}
	if req.ID == 0 {
		idStr := strings.TrimSpace(c.DefaultQuery("id", ""))
		if idStr != "" {
			id, _ := strconv.Atoi(idStr)
			if id > 0 {
				req.ID = uint(id)
			}
		}
	}
	if err := service.ManageSvc.DeleteMappingRule(req.ID); err != nil {
		dto.Failed(fmt.Sprintf("映射规则删除失败: %v", err), c)
		return
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
	return nil
}
