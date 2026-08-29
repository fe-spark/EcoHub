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
	data, err := access.QueryOverview(c.Query("day"))
	if err != nil {
		dto.Failed("访问分析暂不可用", c)
		return
	}
	dto.Success(data, "访问分析概览获取成功", c)
}

func (h *AccessHandler) Tops(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	kind := c.DefaultQuery("kind", "path")
	items, err := access.QueryTops(c.Query("day"), kind, limit)
	if err != nil {
		dto.Failed("访问分析暂不可用", c)
		return
	}
	dto.Success(gin.H{"kind": kind, "items": items}, "访问分析榜单获取成功", c)
}

func (h *AccessHandler) TrackView(c *gin.Context) {
	if c.Request.Body != nil {
		c.Request.Body = http.MaxBytesReader(nil, c.Request.Body, trackViewMaxBody)
	}
	var body struct {
		Action   string `json:"action"`
		Resource string `json:"resource"`
		Source   string `json:"source"`
		Path     string `json:"path"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		dto.SuccessOnlyMsg("ok", c)
		return
	}
	access.TrackPage(c, body.Action, body.Resource, body.Source, body.Path)
	dto.SuccessOnlyMsg("ok", c)
}

func (h *AccessHandler) Logs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "0"))
	list, err := access.QueryLogs(
		c.Query("day"),
		c.DefaultQuery("source", "recent"),
		c.Query("status"),
		c.Query("client"),
		c.Query("q"),
		limit,
	)
	if err != nil {
		dto.Failed("访问分析暂不可用", c)
		return
	}
	dto.Success(gin.H{"list": list}, "访问日志获取成功", c)
}
