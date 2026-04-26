package handler

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

type ManageHandler struct{}

var ManageHd = new(ManageHandler)

func (h *ManageHandler) ManageIndex(c *gin.Context) {
	dto.SuccessOnlyMsg("后台管理中心", c)
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

// ResetSiteBasic 重置网站配置信息为初始化状态
func (h *ManageHandler) ResetSiteBasic(c *gin.Context) {
	if err := service.ManageSvc.ResetSiteBasic(); err != nil {
		dto.Failed(fmt.Sprint("配置信息重置失败: ", err), c)
		return
	}
	dto.SuccessOnlyMsg("配置信息已重置为默认值", c)
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
		dto.Failed("Banners最大阈值为6, 无法添加新的banner信息", c)
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
