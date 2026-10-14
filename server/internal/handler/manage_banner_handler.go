package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

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
	if len(bl) >= model.MaxBannerCount {
		dto.Failed(fmt.Sprintf("已达轮播上限（%d 部）", model.MaxBannerCount), c)
		return
	}
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

// BannerBatchScrape 一键批量对当前排片列表中缺失横图的影片进行 TMDB 刮削（支持 SSE 实时事件流）
func (h *ManageHandler) BannerBatchScrape(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		dto.Failed("当前环境不支持流式推送", c)
		return
	}
	flusher.Flush()

	var writeMu sync.Mutex
	sendEvent := func(evt service.BatchScrapeProgressEvent) {
		writeMu.Lock()
		defer writeMu.Unlock()
		payload, err := json.Marshal(evt)
		if err != nil {
			return
		}
		_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", payload)
		flusher.Flush()
	}

	_ = service.ManageSvc.BatchScrapeBannersStream(c.Request.Context(), sendEvent)
}

// BannerQuickGenerate 一键排片：按选择分类与策略（最热/高分/最新）一键生成排片列表（支持 SSE 实时事件流）
func (h *ManageHandler) BannerQuickGenerate(c *gin.Context) {
	var req service.OneClickGenerateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("排片参数解析异常: "+err.Error(), c)
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		dto.Failed("当前环境不支持流式推送", c)
		return
	}
	flusher.Flush()

	var writeMu sync.Mutex
	sendEvent := func(evt service.OneClickProgressEvent) {
		writeMu.Lock()
		defer writeMu.Unlock()
		payload, err := json.Marshal(evt)
		if err != nil {
			return
		}
		_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", payload)
		flusher.Flush()
	}

	_ = service.ManageSvc.OneClickGenerateDeckStream(c.Request.Context(), req, sendEvent)
}
