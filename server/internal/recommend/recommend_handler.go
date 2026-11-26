package recommend

import (
	"server/internal/model/dto"

	"github.com/gin-gonic/gin"
)

type RecommendHandler struct{}

var RecommendHd = new(RecommendHandler)

func (h *RecommendHandler) List(c *gin.Context) {
	res, err := HomeRecommendSvc.ListAdmin(c.Request.Context(), c.Query("sourceId"))
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(res, "获取首页推荐列表成功", c)
}

func (h *RecommendHandler) InitPresets(c *gin.Context) {
	var req struct {
		SourceId string `json:"sourceId"`
	}
	_ = c.ShouldBindJSON(&req)
	res, err := HomeRecommendSvc.InitPresetGroups(c.Request.Context(), req.SourceId)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(res, "初始化默认推荐分组成功", c)
}

func (h *RecommendHandler) ImportCategories(c *gin.Context) {
	var req struct {
		SourceId string `json:"sourceId"`
	}
	_ = c.ShouldBindJSON(&req)
	res, err := HomeRecommendSvc.ImportCategoryGroups(c.Request.Context(), req.SourceId)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(res, "导入站点分类分组成功", c)
}

func (h *RecommendHandler) CreateGroup(c *gin.Context) {
	var req RecommendGroupInput
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("参数格式错误", c)
		return
	}
	g, err := HomeRecommendSvc.CreateGroup(c.Request.Context(), req)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(g, "创建推荐分组成功", c)
}

func (h *RecommendHandler) UpdateGroup(c *gin.Context) {
	var req RecommendGroupInput
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("参数格式错误", c)
		return
	}
	if err := HomeRecommendSvc.UpdateGroup(c.Request.Context(), c.Param("id"), req); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(nil, "更新推荐分组成功", c)
}

func (h *RecommendHandler) DeleteGroup(c *gin.Context) {
	if err := HomeRecommendSvc.DeleteGroup(c.Request.Context(), c.Param("id")); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(nil, "删除推荐分组成功", c)
}

func (h *RecommendHandler) ReorderGroups(c *gin.Context) {
	var req struct {
		SourceId string   `json:"sourceId"`
		Ids      []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("参数格式错误", c)
		return
	}
	if err := HomeRecommendSvc.ReorderGroups(c.Request.Context(), req.SourceId, req.Ids); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(nil, "分组排序已更新", c)
}

func (h *RecommendHandler) AddItem(c *gin.Context) {
	var req struct {
		Mid         int64  `json:"mid"`
		DoubanScore string `json:"doubanScore"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("参数格式错误", c)
		return
	}
	item, err := HomeRecommendSvc.AddItem(c.Request.Context(), c.Param("id"), req.Mid, req.DoubanScore)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(item, "已加入推荐分组", c)
}

func (h *RecommendHandler) ReorderItems(c *gin.Context) {
	var req struct {
		Ids []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("参数格式错误", c)
		return
	}
	if err := HomeRecommendSvc.ReorderItems(c.Request.Context(), c.Param("id"), req.Ids); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(nil, "推荐影片排序已更新", c)
}

func (h *RecommendHandler) DeleteItem(c *gin.Context) {
	if err := HomeRecommendSvc.DeleteItem(c.Request.Context(), c.Param("itemId")); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(nil, "已移出推荐分组", c)
}

func (h *RecommendHandler) ClearGroupItems(c *gin.Context) {
	if err := HomeRecommendSvc.ClearGroupItems(c.Request.Context(), c.Param("id")); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(nil, "已清空推荐分组", c)
}

func (h *RecommendHandler) SyncGroup(c *gin.Context) {
	res, err := HomeRecommendSvc.SyncGroupNow(c.Request.Context(), c.Param("id"))
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(res, "豆瓣榜单同步成功", c)
}

func (h *RecommendHandler) SyncAll(c *gin.Context) {
	var req struct {
		SourceId string `json:"sourceId"`
	}
	_ = c.ShouldBindJSON(&req)
	res, err := HomeRecommendSvc.SyncSourceChartGroups(c.Request.Context(), req.SourceId)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(res, "榜单批量同步完成", c)
}

func (h *RecommendHandler) Generate(c *gin.Context) {
	var req RecommendGenerateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("参数格式错误", c)
		return
	}
	res, err := HomeRecommendSvc.GenerateQuickPreview(c.Request.Context(), req)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(res, "一键排片预览生成成功", c)
}

func (h *RecommendHandler) PublishDeck(c *gin.Context) {
	var req RecommendDeckSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("参数格式错误", c)
		return
	}
	if err := HomeRecommendSvc.PublishQuickDeck(c.Request.Context(), req); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(nil, "一键排片已发布", c)
}
