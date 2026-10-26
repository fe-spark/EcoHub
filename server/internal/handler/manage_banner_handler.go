package handler

import (
	"strings"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

// BannerList 获取当前首页轮播列表
func (h *ManageHandler) BannerList(c *gin.Context) {
	bl, err := service.ManageSvc.GetBanners()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(bl, "获取轮播列表成功", c)
}

// BannerSave 保存轮播数据（新增或更新）
func (h *ManageHandler) BannerSave(c *gin.Context) {
	var b model.Banner
	if err := c.ShouldBindJSON(&b); err != nil {
		dto.Failed("Banner 参数提交异常", c)
		return
	}
	bl, err := service.ManageSvc.GetBanners()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	b.Id = strings.TrimSpace(b.Id)
	if b.Id == "" {
		id, err := utils.GenerateSalt()
		if err != nil {
			dto.Failed(err.Error(), c)
			return
		}
		b.Id = id
		b.Sort = int64(len(bl) + 1)
		bl = append(bl, b)
		if err := service.ManageSvc.SaveBanners(bl); err != nil {
			dto.Failed("添加轮播失败: "+err.Error(), c)
			return
		}
		dto.SuccessOnlyMsg("添加轮播成功", c)
		return
	}
	found := false
	for i, item := range bl {
		if item.Id == b.Id {
			bl[i] = b
			found = true
			break
		}
	}
	if !found {
		dto.Failed("未匹配到对应轮播项", c)
		return
	}
	if err := service.ManageSvc.SaveBanners(bl); err != nil {
		dto.Failed("更新轮播失败: "+err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("更新轮播成功", c)
}

// BannerDelete 删除轮播数据（批量/单条，数组入参）
func (h *ManageHandler) BannerDelete(c *gin.Context) {
	var req struct {
		Ids []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	idMap := make(map[string]struct{}, len(req.Ids))
	for _, id := range req.Ids {
		id = strings.TrimSpace(id)
		if id != "" {
			idMap[id] = struct{}{}
		}
	}
	if len(idMap) == 0 {
		dto.Failed("缺少 ID 参数", c)
		return
	}
	bl, err := service.ManageSvc.GetBanners()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	newBl := make([]model.Banner, 0, len(bl))
	for _, b := range bl {
		if _, ok := idMap[b.Id]; !ok {
			newBl = append(newBl, b)
		}
	}
	if err := service.ManageSvc.SaveBanners(newBl); err != nil {
		dto.Failed("删除失败: "+err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("删除轮播成功", c)
}

// BannerSaveDeck 批量保存排片卡组与排序
func (h *ManageHandler) BannerSaveDeck(c *gin.Context) {
	var req model.Banners
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("排片卡组数据格式异常", c)
		return
	}
	if err := service.ManageSvc.SaveBannerDeck(req); err != nil {
		dto.Failed("保存排片卡组失败: "+err.Error(), c)
		return
	}
	banners, err := service.ManageSvc.GetBanners()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(banners, "排片卡组已保存并生效", c)
}

// BannerQuickConfigGet 读取已保存的一键排片配置。
func (h *ManageHandler) BannerQuickConfigGet(c *gin.Context) {
	view, err := service.ManageSvc.GetBannerQuickConfig()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(view, "获取排片配置成功", c)
}

// BannerQuickConfigSave 保存一键排片配置。不生成卡组，不替换线上轮播。
func (h *ManageHandler) BannerQuickConfigSave(c *gin.Context) {
	var req service.BannerQuickConfigInput
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("排片配置参数异常", c)
		return
	}
	view, err := service.ManageSvc.SaveBannerQuickConfig(req)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(view, "配置已保存", c)
}

// BannerQuickGenerate 一键排片：按选择分类与策略（最热/高分/最新）一键生成排片列表（支持 SSE 实时事件流）
func (h *ManageHandler) BannerQuickGenerate(c *gin.Context) {
	var req service.OneClickGenerateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("排片参数解析异常: "+err.Error(), c)
		return
	}

	stream, ok := openSSE(c)
	if !ok {
		return
	}
	if err := service.ManageSvc.OneClickGenerateDeckStream(c.Request.Context(), req, func(evt service.OneClickProgressEvent) {
		_ = stream.SendData(evt)
	}); err != nil {
		_ = stream.SendData(gin.H{"error": err.Error()})
	}
}
