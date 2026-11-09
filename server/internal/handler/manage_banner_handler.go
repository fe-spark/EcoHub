package handler

import (
	"net/http"

	"server/internal/model/dto"
	"server/internal/service"

	"github.com/gin-gonic/gin"
)

// bannerConflictCode 版本号冲突时响应体里的 code，HTTP 状态为 409。
const bannerConflictCode = http.StatusConflict

func bannerFail(err error, c *gin.Context) {
	switch {
	case service.IsBannerConflict(err):
		dto.CustomResult(http.StatusConflict, bannerConflictCode, nil, err.Error(), c)
	case service.IsBannerUserError(err):
		dto.Failed(err.Error(), c)
	default:
		dto.Failed("操作失败: "+err.Error(), c)
	}
}

// BannerList GET /manage/banners?sourceId= 一个站点的轮播，不传时用首选站。
func (h *ManageHandler) BannerList(c *gin.Context) {
	view, err := service.BannerSvc.List(c.Query("sourceId"))
	if err != nil {
		bannerFail(err, c)
		return
	}
	dto.Success(view, "获取轮播列表成功", c)
}

type bannerCreateReq struct {
	SourceId string `json:"sourceId"`
	service.BannerFields
}

// BannerCreate POST /manage/banners 新增一行，立即生效。
func (h *ManageHandler) BannerCreate(c *gin.Context) {
	var req bannerCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	item, err := service.BannerSvc.Create(req.SourceId, req.BannerFields)
	if err != nil {
		bannerFail(err, c)
		return
	}
	dto.Success(item, "添加轮播成功", c)
}

// BannerUpdate PATCH /manage/banners/:id 只改提交了的字段，立即生效。
// 空字符串清除文字/图片覆盖；score、year 填 0 等于清除覆盖、显示片库值。换片时刮削和覆盖一并清除。
func (h *ManageHandler) BannerUpdate(c *gin.Context) {
	var req service.BannerFields
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	item, err := service.BannerSvc.Update(c.Param("id"), req)
	if err != nil {
		bannerFail(err, c)
		return
	}
	dto.Success(item, "更新轮播成功", c)
}

// BannerDelete DELETE /manage/banners/:id 删除一行，立即生效。
func (h *ManageHandler) BannerDelete(c *gin.Context) {
	if err := service.BannerSvc.Delete(c.Param("id")); err != nil {
		bannerFail(err, c)
		return
	}
	dto.SuccessOnlyMsg("删除轮播成功", c)
}

type bannerOrderReq struct {
	SourceId string   `json:"sourceId"`
	Revision string   `json:"revision"`
	Ids      []string `json:"ids"`
}

// BannerOrder PUT /manage/banners/order 只提交顺序。revision 不一致返回 409。
func (h *ManageHandler) BannerOrder(c *gin.Context) {
	var req bannerOrderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	view, err := service.BannerSvc.Reorder(req.SourceId, req.Revision, req.Ids)
	if err != nil {
		bannerFail(err, c)
		return
	}
	dto.Success(view, "排序已保存", c)
}

// BannerGenerate POST /manage/banners/generate 按提交的策略和数量生成预览，不落库。
func (h *ManageHandler) BannerGenerate(c *gin.Context) {
	var req service.BannerGenerateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	result, err := service.BannerSvc.Generate(c.Request.Context(), req)
	if err != nil {
		bannerFail(err, c)
		return
	}
	dto.Success(result, "排片完成", c)
}

type bannerDeckReq struct {
	SourceId string                 `json:"sourceId"`
	Revision string                 `json:"revision"`
	Items    []service.BannerFields `json:"items"`
}

// BannerDeck PUT /manage/banners/deck 发布排片，整组替换该站轮播。revision 不一致返回 409。
func (h *ManageHandler) BannerDeck(c *gin.Context) {
	var req bannerDeckReq
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	view, err := service.BannerSvc.PublishDeck(req.SourceId, req.Revision, req.Items)
	if err != nil {
		bannerFail(err, c)
		return
	}
	dto.Success(view, "排片已发布", c)
}
