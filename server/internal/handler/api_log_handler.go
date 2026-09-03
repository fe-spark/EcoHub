package handler

import (
	"strconv"

	"server/internal/access"
	"server/internal/model/dto"

	"github.com/gin-gonic/gin"
)

type ApiLogHandler struct{}

var ApiLogHd = new(ApiLogHandler)

// List 分页查询接口访问记录
func (h *ApiLogHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))

	params := access.ApiLogQueryParams{
		Page:       page,
		PageSize:   pageSize,
		Day:        c.Query("day"),
		StartTime:  c.Query("startTime"),
		EndTime:    c.Query("endTime"),
		Method:     c.Query("method"),
		Status:     c.Query("status"),
		Duration:   c.Query("duration"),
		ClientType: c.Query("clientType"),
		Q:          c.Query("q"),
	}

	result, err := access.QueryApiAccessLogs(params)
	if err != nil {
		dto.Failed("获取接口访问记录失败", c)
		return
	}
	dto.Success(result, "获取接口访问记录成功", c)
}

// Prune 手动清理过期接口访问记录
func (h *ApiLogHandler) Prune(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	deleted, err := access.PruneExpiredApiLogs(days)
	if err != nil {
		dto.Failed("清理过期接口访问记录失败", c)
		return
	}
	dto.Success(gin.H{"deleted": deleted}, "清理过期接口访问记录成功", c)
}
