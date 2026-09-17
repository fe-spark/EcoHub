package handler

import (
	"strconv"
	"strings"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"

	"github.com/gin-gonic/gin"
)

type TMDBHandler struct{}

var TMDBHd = new(TMDBHandler)

// GetConfig 获取 TMDB 配置
func (h *TMDBHandler) GetConfig(c *gin.Context) {
	cfg := service.TMDBSvc.GetConfig()
	dto.Success(cfg, "获取 TMDB 配置成功", c)
}

// UpdateConfig 更新 TMDB 配置
func (h *TMDBHandler) UpdateConfig(c *gin.Context) {
	var cfg model.TMDBConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		dto.Failed("请求参数格式异常", c)
		return
	}
	if err := service.TMDBSvc.UpdateConfig(cfg); err != nil {
		dto.Failed("保存 TMDB 配置失败: "+err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("TMDB 配置保存成功", c)
}

// TestConfig 测试 TMDB 连通性
func (h *TMDBHandler) TestConfig(c *gin.Context) {
	var cfg model.TMDBConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		dto.Failed("请求参数格式异常", c)
		return
	}
	if err := service.TMDBSvc.TestConnection(cfg); err != nil {
		dto.Failed("连接失败: "+err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("TMDB 连接测试成功，API Key 校验通过！", c)
}

// Search 检索 TMDB 候选列表
func (h *TMDBHandler) Search(c *gin.Context) {
	query := strings.TrimSpace(c.Query("query"))
	if query == "" {
		dto.Failed("搜索关键词不能为空", c)
		return
	}
	year := strings.TrimSpace(c.Query("year"))
	mediaType := strings.TrimSpace(c.Query("type"))

	candidates, err := service.TMDBSvc.Search(query, year, mediaType)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(candidates, "检索成功", c)
}

// Apply 应用 TMDB 刮削数据到影片
func (h *TMDBHandler) Apply(c *gin.Context) {
	var req model.TMDBApplyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数格式异常", c)
		return
	}
	if req.Mid <= 0 {
		dto.Failed("缺少影片 ID", c)
		return
	}
	if req.TmdbID <= 0 {
		dto.Failed("缺少 TMDB ID", c)
		return
	}

	if err := service.TMDBSvc.ApplyDetail(req); err != nil {
		dto.Failed("刮削数据应用失败: "+err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("TMDB 元数据已成功应用并更新入库！", c)
}

// Prefill 获取 TMDB 详情用于表单自动填充
func (h *TMDBHandler) Prefill(c *gin.Context) {
	idStr := strings.TrimSpace(c.Query("id"))
	tmdbID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || tmdbID <= 0 {
		dto.Failed("TMDB ID 参数异常", c)
		return
	}
	mediaType := strings.TrimSpace(c.Query("type"))

	data, err := service.TMDBSvc.FetchFormPrefill(tmdbID, mediaType)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(data, "获取详情成功", c)
}
