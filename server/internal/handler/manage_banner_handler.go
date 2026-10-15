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
	bl := service.ManageSvc.GetBanners()
	dto.Success(bl, "获取轮播列表成功", c)
}

// BannerAdd 手动添加影片到轮播
func (h *ManageHandler) BannerAdd(c *gin.Context) {
	var b model.Banner
	if err := c.ShouldBindJSON(&b); err != nil {
		dto.Failed("Banner 参数提交异常", c)
		return
	}
	b.Id = utils.GenerateSalt()
	bl := service.ManageSvc.GetBanners()
	b.Sort = int64(len(bl) + 1)
	bl = append(bl, b)
	if err := service.ManageSvc.SaveBanners(bl); err != nil {
		dto.Failed("添加轮播失败: "+err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("添加轮播成功", c)
}

// BannerUpdate 更新轮播数据
func (h *ManageHandler) BannerUpdate(c *gin.Context) {
	var banner model.Banner
	if err := c.ShouldBindJSON(&banner); err != nil {
		dto.Failed("Banner 参数提交异常", c)
		return
	}
	bl := service.ManageSvc.GetBanners()
	for i, b := range bl {
		if b.Id == banner.Id {
			bl[i] = banner
			if err := service.ManageSvc.SaveBanners(bl); err != nil {
				dto.Failed("更新轮播失败: "+err.Error(), c)
			} else {
				dto.SuccessOnlyMsg("更新轮播成功", c)
				return
			}
		}
	}
	dto.Failed("未匹配到对应轮播项", c)
}

// BannerDel 删除轮播数据
func (h *ManageHandler) BannerDel(c *gin.Context) {
	var req struct {
		Id string `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	id := strings.TrimSpace(req.Id)
	if id == "" {
		dto.Failed("缺少 ID 参数", c)
		return
	}
	bl := service.ManageSvc.GetBanners()
	for i, b := range bl {
		if b.Id == id {
			bl = append(bl[:i], bl[i+1:]...)
			_ = service.ManageSvc.SaveBanners(bl)
			dto.SuccessOnlyMsg("删除轮播成功", c)
			return
		}
	}
	dto.Failed("删除失败，未找到该轮播项", c)
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
	dto.Success(service.ManageSvc.GetBanners(), "排片卡组已保存并生效", c)
}

// BannerQuickConfigGet 读取已保存的一键排片配方。
func (h *ManageHandler) BannerQuickConfigGet(c *gin.Context) {
	dto.Success(service.ManageSvc.GetBannerQuickConfig(), "获取排片配置成功", c)
}

// BannerQuickConfigSave 保存一键排片配方。不生成卡组，不替换线上轮播。
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
	_ = service.ManageSvc.OneClickGenerateDeckStream(c.Request.Context(), req, func(evt service.OneClickProgressEvent) {
		_ = stream.SendData(evt)
	})
}

// BannerPreviewScrape 排片草稿批量刮削预览。不写片库，确认替换后才落库。
func (h *ManageHandler) BannerPreviewScrape(c *gin.Context) {
	var req service.PreviewScrapeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("刮削参数解析异常", c)
		return
	}

	stream, ok := openSSE(c)
	if !ok {
		return
	}
	_ = service.ManageSvc.PreviewScrapeDeckStream(c.Request.Context(), req.Mids, func(evt service.PreviewScrapeEvent) {
		_ = stream.SendData(evt)
	})
}
