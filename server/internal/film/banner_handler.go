package film

import (
	"errors"
	"net/http"

	"server/internal/model/dto"
	"server/internal/repobase"

	"github.com/gin-gonic/gin"
)

// bannerConflictCode 版本号冲突时响应体里的 code，HTTP 状态为 409。
const bannerConflictCode = http.StatusConflict

type BannerHandler struct{}

var BannerHd = new(BannerHandler)

func bannerFail(err error, c *gin.Context) {
	switch {
	case errors.Is(err, repobase.ErrPrimaryMismatch):
		dto.PrimaryMismatch(c)
	case IsBannerConflict(err):
		dto.CustomResult(http.StatusConflict, bannerConflictCode, nil, err.Error(), c)
	case IsBannerUserError(err):
		dto.Failed(err.Error(), c)
	default:
		dto.Failed("操作失败: "+err.Error(), c)
	}
}

// BannerList GET /manage/banners 获取指定采集站轮播（sourceId 为空时默认当前主站）。
func (h *BannerHandler) BannerList(c *gin.Context) {
	view, err := BannerSvc.List(c.Query("sourceId"))
	if err != nil {
		bannerFail(err, c)
		return
	}
	dto.Success(view, "获取轮播列表成功", c)
}

// BannerCategories GET /manage/banners/categories 获取指定采集站可用于一键排片的一级分类。
func (h *BannerHandler) BannerCategories(c *gin.Context) {
	list, err := BannerSvc.Categories(c.Query("sourceId"))
	if err != nil {
		bannerFail(err, c)
		return
	}
	dto.Success(list, "获取轮播分类成功", c)
}

// BannerFilms GET /manage/banners/films 按采集站检索可供轮播选取的影片。
func (h *BannerHandler) BannerFilms(c *gin.Context) {
	page := dto.GetPageParams(c)
	if c.Query("pageSize") == "" && c.Query("pagesize") == "" && c.Query("limit") == "" && c.Query("size") == "" {
		page.PageSize = 10
	}
	list, resolvedSourceID, err := BannerSvc.SearchFilms(
		c.Request.Context(),
		c.Query("sourceId"),
		c.Query("name"),
		c.Query("sort"),
		page,
	)
	if err != nil {
		bannerFail(err, c)
		return
	}
	dto.Success(gin.H{
		"sourceId": resolvedSourceID,
		"list":     list,
		"params": gin.H{
			"paging": page,
		},
	}, "获取影片列表成功", c)
}

// BannerCreate POST /manage/banners 新增一行，立即生效。
func (h *BannerHandler) BannerCreate(c *gin.Context) {
	var req struct {
		SourceId string `json:"sourceId"`
		BannerFields
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	item, err := BannerSvc.Create(req.SourceId, req.BannerFields)
	if err != nil {
		bannerFail(err, c)
		return
	}
	dto.Success(item, "添加轮播成功", c)
}

// BannerUpdate PATCH /manage/banners/:id 只改提交了的字段，立即生效。
// 空字符串清除文字/图片覆盖；score、year 填 0 等于清除覆盖、显示片库值。换片时刮削和覆盖一并清除。
func (h *BannerHandler) BannerUpdate(c *gin.Context) {
	var req BannerFields
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	item, err := BannerSvc.Update(c.Param("id"), c.Query("sourceId"), req)
	if err != nil {
		bannerFail(err, c)
		return
	}
	dto.Success(item, "更新轮播成功", c)
}

// BannerDelete DELETE /manage/banners/:id 删除一行，立即生效。
func (h *BannerHandler) BannerDelete(c *gin.Context) {
	if err := BannerSvc.Delete(c.Param("id"), c.Query("sourceId")); err != nil {
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
func (h *BannerHandler) BannerOrder(c *gin.Context) {
	var req bannerOrderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	view, err := BannerSvc.Reorder(req.SourceId, req.Revision, req.Ids)
	if err != nil {
		bannerFail(err, c)
		return
	}
	dto.Success(view, "排序已保存", c)
}

// BannerGenerate POST /manage/banners/generate 按提交的策略和数量生成预览，不落库。
func (h *BannerHandler) BannerGenerate(c *gin.Context) {
	var req BannerGenerateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	result, err := BannerSvc.Generate(c.Request.Context(), req)
	if err != nil {
		bannerFail(err, c)
		return
	}
	dto.Success(result, "排片完成", c)
}

type bannerDeckReq struct {
	SourceId string                 `json:"sourceId"`
	Revision string                 `json:"revision"`
	Items    []BannerFields `json:"items"`
}

// BannerDeck PUT /manage/banners/deck 发布排片，整组替换指定采集站轮播。revision 不一致返回 409。
func (h *BannerHandler) BannerDeck(c *gin.Context) {
	var req bannerDeckReq
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	view, err := BannerSvc.PublishDeck(req.SourceId, req.Revision, req.Items)
	if err != nil {
		bannerFail(err, c)
		return
	}
	dto.Success(view, "排片已发布", c)
}
