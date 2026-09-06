package handler

import (
	"net/http"
	"strconv"

	"server/internal/access"
	"server/internal/config"
	"server/internal/model/dto"

	"github.com/gin-gonic/gin"
)

const trackViewMaxBody = 4 << 10

type AccessHandler struct{}

var AccessHd = new(AccessHandler)

// Status 查询数据分析功能开启状态
func (h *AccessHandler) Status(c *gin.Context) {
	dto.Success(gin.H{"enabled": config.AccessLogEnabled}, "数据分析状态获取成功", c)
}

// ManualRollup 手动触发分析数据持久化入库
func (h *AccessHandler) ManualRollup(c *gin.Context) {
	if !config.AccessLogEnabled {
		dto.Failed("数据分析功能未开启", c)
		return
	}
	if err := access.ManualRollup(); err != nil {
		dto.Failed("数据分析落库失败: "+err.Error(), c)
		return
	}
	dto.Success(nil, "数据分析已成功落库", c)
}

func (h *AccessHandler) Overview(c *gin.Context) {
	if !config.AccessLogEnabled {
		dto.Failed("数据分析功能未开启", c)
		return
	}
	data, err := access.QueryOverviewScope(c.Query("day"), c.Query("module"), c.Query("platform"))
	if err != nil {
		dto.Failed("数据分析暂不可用", c)
		return
	}
	dto.Success(data, "数据分析概览获取成功", c)
}

func (h *AccessHandler) Tops(c *gin.Context) {
	if !config.AccessLogEnabled {
		dto.Failed("数据分析功能未开启", c)
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	kind := c.DefaultQuery("kind", "path")
	items, err := access.QueryTopsScope(c.Query("day"), kind, c.Query("module"), c.Query("platform"), limit)
	if err != nil {
		dto.Failed("数据分析暂不可用", c)
		return
	}
	dto.Success(gin.H{"kind": kind, "items": items}, "数据分析榜单获取成功", c)
}

func (h *AccessHandler) TrackView(c *gin.Context) {
	if !config.AccessLogEnabled {
		dto.SuccessOnlyMsg("ok", c)
		return
	}
	if c.Request.Body != nil {
		c.Request.Body = http.MaxBytesReader(nil, c.Request.Body, trackViewMaxBody)
	}
	var body access.TrackViewPayload
	if err := c.ShouldBindJSON(&body); err != nil {
		dto.SuccessOnlyMsg("ok", c)
		return
	}
	access.TrackPagePayload(c, body)
	dto.SuccessOnlyMsg("ok", c)
}

func (h *AccessHandler) Logs(c *gin.Context) {
	if !config.AccessLogEnabled {
		dto.Failed("数据分析功能未开启", c)
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "0"))
	list, err := access.QueryLogsScope(
		c.Query("day"),
		c.DefaultQuery("source", "recent"),
		c.Query("status"),
		c.Query("client"),
		c.Query("q"),
		c.Query("module"),
		c.Query("platform"),
		limit,
	)
	if err != nil {
		dto.Failed("数据分析暂不可用", c)
		return
	}
	dto.Success(gin.H{"list": list}, "数据流转日志获取成功", c)
}
