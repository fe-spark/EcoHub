package handler

import (
	"fmt"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/notify"

	"github.com/gin-gonic/gin"
)

type NotifyHandler struct{}

var NotifyHd = new(NotifyHandler)

// GetNotifyConfig 获取通知配置（Token 脱敏）。
func (h *NotifyHandler) GetNotifyConfig(c *gin.Context) {
	cfg := notify.PublicConfig(notify.GetConfig())
	dto.Success(cfg, "通知配置获取成功", c)
}

// UpdateNotifyConfig 更新通知配置。
func (h *NotifyHandler) UpdateNotifyConfig(c *gin.Context) {
	var incoming model.NotifyConfig
	if err := c.ShouldBindJSON(&incoming); err != nil {
		dto.Failed(fmt.Sprint("请求参数异常: ", err), c)
		return
	}
	old := notify.GetConfig()
	merged, err := notify.ValidateAndMergeUpdate(old, incoming)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	if err := notify.SaveConfig(merged); err != nil {
		dto.Failed(fmt.Sprint("通知配置保存失败: ", err), c)
		return
	}
	dto.Success(notify.PublicConfig(merged), "通知配置已保存", c)
}

// notifyTestReq 测试发送可选草稿（不落库）。空字段沿用已保存配置。
type notifyTestReq struct {
	BotToken string   `json:"botToken"`
	ChatIDs  []string `json:"chatIds"`
}

// TestNotify 发送测试消息。可带表单草稿 Token/Chat，无需先保存。
func (h *NotifyHandler) TestNotify(c *gin.Context) {
	var req notifyTestReq
	_ = c.ShouldBindJSON(&req)

	result, err := notify.SendTestWith(req.BotToken, req.ChatIDs)
	if err != nil {
		// 附带各 Chat 失败明细，便于前端展示具体 Telegram 原因（如 chat not found）。
		dto.FailedWithData(result, err.Error(), c)
		return
	}
	dto.Success(result, "测试消息已发送", c)
}
