package handler

import (
	"net/http"
	"strconv"

	"server/internal/access"
	"server/internal/model/dto"

	"github.com/gin-gonic/gin"
)

const trackViewMaxBody = 4 << 10

type AccessHandler struct{}

var AccessHd = new(AccessHandler)

func (h *AccessHandler) Overview(c *gin.Context) {
	data, err := access.QueryOverviewScope(c.Query("day"), c.Query("module"), c.Query("platform"))
	if err != nil {
		dto.Failed("数据分析暂不可用", c)
		return
	}
	dto.Success(data, "数据分析概览获取成功", c)
}

func (h *AccessHandler) Tops(c *gin.Context) {
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
