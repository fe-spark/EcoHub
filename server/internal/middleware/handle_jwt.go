package middleware

import (
	"net/http"
	"server/internal/config"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

// AuthToken 用户登录Token拦截（单点登录校验）
func AuthToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		authToken := ReadAuthToken(c)
		if authToken == "" {
			ClearAuthCookie(c)
			dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "用户未授权,请先登录", c)
			c.Abort()
			return
		}
		uc, err := utils.ParseToken(authToken)
		if err != nil || uc == nil {
			ClearAuthCookie(c)
			dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "身份验证信息已失效,请重新登录!!!", c)
			c.Abort()
			return
		}

		storedToken := service.UserSvc.StoredToken(uc.UserID)
		if storedToken == "" {
			ClearAuthCookie(c)
			dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "身份验证信息已失效,请重新登录!!!", c)
			c.Abort()
			return
		}
		if storedToken != authToken {
			ClearAuthCookie(c)
			dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "账号在其它设备登录,身份验证信息失效,请重新登录!!!", c)
			c.Abort()
			return
		}

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
