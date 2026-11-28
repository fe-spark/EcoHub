package middleware

import (
	"net/http"
	"server/internal/config"
	"server/internal/httpx"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/user"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

// AuthToken 用户登录Token拦截（非访客单点互踢，访客支持多点登录）
func AuthToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		authToken := httpx.ReadAuthToken(c)
		if authToken == "" {
			httpx.ClearAuthCookie(c)
			dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "用户未授权,请先登录", c)
			c.Abort()
			return
		}
		uc, err := utils.ParseToken(authToken)
		if err != nil || uc == nil {
			httpx.ClearAuthCookie(c)
			dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "身份验证信息已失效,请重新登录!!!", c)
			c.Abort()
			return
		}

		tokenStatus, err := user.UserSvc.VerifyToken(uc.UserID, uc.Role, authToken)
		if err != nil {
			dto.CustomResult(http.StatusServiceUnavailable, dto.FAILED, nil, "登录状态暂不可用", c)
			c.Abort()
			return
		}
		switch tokenStatus {
		case user.TokenMissing:
			httpx.ClearAuthCookie(c)
			dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "身份验证信息已失效,请重新登录!!!", c)
			c.Abort()
			return
		case user.TokenSuperseded:
			httpx.ClearAuthCookie(c)
			dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "账号在其它设备登录,身份验证信息失效,请重新登录!!!", c)
			c.Abort()
			return
		}
		status, role, err := user.UserSvc.AccountGate(uc.UserID)
		if err != nil {
			dto.CustomResult(http.StatusServiceUnavailable, dto.FAILED, nil, "登录状态暂不可用", c)
			c.Abort()
			return
		}
		if status != 0 {
			_ = user.UserSvc.ClearUserSessions(uc.UserID)
			httpx.ClearAuthCookie(c)
			dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "账号已被禁用", c)
			c.Abort()
			return
		}
		// 若数据库中角色跨访客/非访客边界发生变化，拒绝旧模式令牌并要求重新登录
		if model.UserAllowsMultiLogin(uc.Role) != model.UserAllowsMultiLogin(role) {
			_ = user.UserSvc.UserLogout(uc.UserID, uc.Role, authToken)
			httpx.ClearAuthCookie(c)
			dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "身份验证信息已失效,请重新登录!!!", c)
			c.Abort()
			return
		}
		uc.Role = role

		// 将UserClaims存放到context中
		c.Set(config.AuthUserClaims, uc)
		c.Next()
	}
}

// WriteAccess 访客账号只允许读取数据
func WriteAccess() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}

		v, ok := c.Get(config.AuthUserClaims)
		if !ok {
			dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "鉴权失败,请重新登录", c)
			c.Abort()
			return
		}
		uc, ok := v.(*utils.UserClaims)
		if !ok {
			dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "鉴权失败,请重新登录", c)
			c.Abort()
			return
		}
		if !model.UserCanWrite(uc.Role) {
			dto.CustomResult(http.StatusForbidden, dto.FAILED, nil, "访客账号仅允许查看，禁止修改数据", c)
			c.Abort()
			return
		}

		c.Next()
	}
}

// AdminAccess 仅允许超级管理员访问
func AdminAccess() gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := c.Get(config.AuthUserClaims)
		if !ok {
			dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "鉴权失败,请重新登录", c)
			c.Abort()
			return
		}
		uc, ok := v.(*utils.UserClaims)
		if !ok || uc == nil {
			dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "鉴权失败,请重新登录", c)
			c.Abort()
			return
		}
		if !model.IsAdmin(uc.UserID, uc.Role) {
			dto.CustomResult(http.StatusForbidden, dto.FAILED, nil, "权限不足，仅超级管理员可执行此操作", c)
			c.Abort()
			return
		}

		c.Next()
	}
}
