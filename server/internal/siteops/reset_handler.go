package siteops

import (
	"server/internal/httpx"
	"server/internal/model/dto"
	"server/internal/user"

	"github.com/gin-gonic/gin"
)

type ResetHandler struct{}

var ResetHd = new(ResetHandler)

// ClearAllFilm 校验通过后以 SSE 推送数据重置进度。重置在后台跑完，连接断开不中止。
func (h *ResetHandler) ClearAllFilm(c *gin.Context) {
	if _, ok := httpx.RequireAdminClaims(c, "权限不足，仅超级管理员可执行数据重置"); !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常!!!", c)
		return
	}
	if !httpx.VerifyCurrentPassword(c, req.Password, user.UserSvc.VerifyUserPassword) {
		dto.Failed("重置失败, 密钥校验失败!!!", c)
		return
	}
	if ResetSvc.SiteResetRunning() {
		dto.Failed("站点数据正在重置中，请勿重复操作", c)
		return
	}
	stream, ok := httpx.OpenSSE(c)
	if !ok {
		return
	}
	if err := ResetSvc.StreamSiteReset(c.Request.Context(), stream.SendData); err != nil {
		_ = stream.SendData(map[string]any{"running": false, "error": err.Error()})
	}
}

// ResetProgressStream 订阅正在进行的数据重置进度。刷新页面后靠它接上。
func (h *ResetHandler) ResetProgressStream(c *gin.Context) {
	stream, ok := httpx.OpenSSE(c)
	if !ok {
		return
	}
	_ = ResetSvc.FollowSiteReset(c.Request.Context(), stream.SendData)
}
