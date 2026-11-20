package handler

import (
	"net/http"

	"server/internal/config"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

// currentClaims 从 Gin 上下文提取当前登录用户声明
func currentClaims(c *gin.Context) (*utils.UserClaims, bool) {
	v, ok := c.Get(config.AuthUserClaims)
	if !ok {
		return nil, false
	}
	uc, ok := v.(*utils.UserClaims)
	if !ok || uc == nil {
		return nil, false
	}
	return uc, true
}

// isAdminRequest 判断当前请求是否由超级管理员发起
func isAdminRequest(c *gin.Context) bool {
	uc, ok := currentClaims(c)
	return ok && model.IsAdmin(uc.UserID, uc.Role)
}

// requireAdminClaims 校验当前请求必须为超级管理员，否则直接写入 401/403 响应
func requireAdminClaims(c *gin.Context, forbiddenMsg string) (*utils.UserClaims, bool) {
	v, exists := c.Get(config.AuthUserClaims)
	if !exists {
		dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "鉴权失败,请重新登录", c)
		return nil, false
	}
	uc, ok := v.(*utils.UserClaims)
	if !ok || uc == nil || !model.IsAdmin(uc.UserID, uc.Role) {
		dto.CustomResult(http.StatusForbidden, dto.FAILED, nil, forbiddenMsg, c)
		return nil, false
	}
	return uc, true
}
