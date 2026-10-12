package handler

import (
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"

	"github.com/gin-gonic/gin"
)

// NotifyHandler Telegram 消息通知协议处理层
type NotifyHandler struct{}

// NotifyHd 通知 Handler 全局单例
var NotifyHd = new(NotifyHandler)

// GetConfig 获取脱敏后的 Telegram 通知配置
func (h *NotifyHandler) GetConfig(c *gin.Context) {
	dto.Success(service.NotifySvc.PublicConfig(), "获取通知配置成功", c)
}

// UpdateConfig 更新 Telegram 通知配置（仅超级管理员）
func (h *NotifyHandler) UpdateConfig(c *gin.Context) {
	var incoming model.NotifyConfig
	if err := c.ShouldBindJSON(&incoming); err != nil {
		dto.Failed("请求参数格式异常", c)
		return
	}
	updated, err := service.NotifySvc.UpdateConfig(incoming)
	if err != nil {
		dto.Failed("保存失败: "+err.Error(), c)
		return
	}
	dto.Success(updated, "通知配置已保存", c)
}

// TestNotify 发送测试通知（仅超级管理员，支持草稿参数）
func (h *NotifyHandler) TestNotify(c *gin.Context) {
	var req model.NotifyTestReq
	_ = c.ShouldBindJSON(&req)

	res, err := service.NotifySvc.SendTest(req)
	if err != nil {
		dto.FailedWithData(res, err.Error(), c)
		return
	}
	dto.Success(res, "测试消息已发送", c)
}
