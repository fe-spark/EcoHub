package handler

import (
	"fmt"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"

	"github.com/gin-gonic/gin"
)

type ProxyHandler struct{}

var ProxyHd = new(ProxyHandler)

func (h *ProxyHandler) GetConfig(c *gin.Context) {
	dto.Success(service.ProxySvc.PublicConfig(), "获取代理配置成功", c)
}

func (h *ProxyHandler) UpdateConfig(c *gin.Context) {
	var body struct {
		model.ProxyConfig
		PreserveAuth bool `json:"preserveAuth"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		dto.Failed("请求参数格式异常", c)
		return
	}
	if err := service.ProxySvc.UpdateConfig(body.ProxyConfig, body.PreserveAuth); err != nil {
		dto.Failed("保存失败: "+err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("代理配置保存成功", c)
}

func (h *ProxyHandler) TestProxy(c *gin.Context) {
	var req model.ProxyTestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数格式异常", c)
		return
	}
	latency, err := service.ProxySvc.TestProxy(req.ProxyURL, req.Target)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(gin.H{"latency": latency}, fmt.Sprintf("代理测试成功 (%dms)", latency), c)
}
