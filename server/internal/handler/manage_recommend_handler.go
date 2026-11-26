package handler

import (
	"server/internal/model/dto"
	"server/internal/service"

	"github.com/gin-gonic/gin"
)

type ManageRecommendHandler struct{}

var ManageRecommendHd = new(ManageRecommendHandler)

func (h *ManageRecommendHandler) List(c *gin.Context) {
	res, err := service.HomeRecommendSvc.ListAdmin(c.Request.Context(), c.Query("sourceId"))
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(res, "获取首页推荐列表成功", c)
}

func (h *ManageRecommendHandler) InitPresets(c *gin.Context) {
	var req struct {
		SourceId string `json:"sourceId"`
	}
	_ = c.ShouldBindJSON(&req)
	groups, err := service.HomeRecommendSvc.InitPresetGroups(c.Request.Context(), req.SourceId)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(groups, "初始化默认推荐分组成功", c)
}

func (h *ManageRecommendHandler) CreateGroup(c *gin.Context) {
	var req service.RecommendGroupInput
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("参数格式错误", c)
		return
	}
	g, err := service.HomeRecommendSvc.CreateGroup(c.Request.Context(), req)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(g, "创建推荐分组成功", c)
}

func (h *ManageRecommendHandler) UpdateGroup(c *gin.Context) {
	var req service.RecommendGroupInput
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("参数格式错误", c)
		return
	}
	if err := service.HomeRecommendSvc.UpdateGroup(c.Request.Context(), c.Param("id"), req); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(nil, "更新推荐分组成功", c)
}

func (h *ManageRecommendHandler) DeleteGroup(c *gin.Context) {
	if err := service.HomeRecommendSvc.DeleteGroup(c.Request.Context(), c.Param("id")); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(nil, "删除推荐分组成功", c)
}

func (h *ManageRecommendHandler) ReorderGroups(c *gin.Context) {
	var req struct {
		SourceId string   `json:"sourceId"`
		Ids      []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("参数格式错误", c)
		return
	}
	if err := service.HomeRecommendSvc.ReorderGroups(c.Request.Context(), req.SourceId, req.Ids); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(nil, "分组排序已更新", c)
}

func (h *ManageRecommendHandler) AddItem(c *gin.Context) {
	var req struct {
		Mid         int64  `json:"mid"`
		DoubanScore string `json:"doubanScore"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("参数格式错误", c)
		return
	}
	item, err := service.HomeRecommendSvc.AddItem(c.Request.Context(), c.Param("id"), req.Mid, req.DoubanScore)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(item, "已加入推荐分组", c)
}

func (h *ManageRecommendHandler) ReorderItems(c *gin.Context) {
	var req struct {
		Ids []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("参数格式错误", c)
		return
	}
	if err := service.HomeRecommendSvc.ReorderItems(c.Request.Context(), c.Param("id"), req.Ids); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(nil, "推荐影片排序已更新", c)
}

func (h *ManageRecommendHandler) DeleteItem(c *gin.Context) {
	if err := service.HomeRecommendSvc.DeleteItem(c.Request.Context(), c.Param("itemId")); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(nil, "已移出推荐分组", c)
}

func (h *ManageRecommendHandler) ClearGroupItems(c *gin.Context) {
	if err := service.HomeRecommendSvc.ClearGroupItems(c.Request.Context(), c.Param("id")); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(nil, "已清空推荐分组", c)
}

func (h *ManageRecommendHandler) SyncGroup(c *gin.Context) {
	if err := service.HomeRecommendSvc.SyncGroupNow(c.Request.Context(), c.Param("id")); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(nil, "豆瓣榜单同步成功", c)
}

func (h *ManageRecommendHandler) Generate(c *gin.Context) {
	var req service.RecommendGenerateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("参数格式错误", c)
		return
	}
	res, err := service.HomeRecommendSvc.GenerateQuickPreview(c.Request.Context(), req)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(res, "一键排片预览生成成功", c)
}

func (h *ManageRecommendHandler) PublishDeck(c *gin.Context) {
	var req service.RecommendDeckSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("参数格式错误", c)
		return
	}
	if err := service.HomeRecommendSvc.PublishQuickDeck(c.Request.Context(), req); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(nil, "一键排片已发布", c)
}
