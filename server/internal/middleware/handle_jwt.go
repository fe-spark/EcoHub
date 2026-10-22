package middleware

import (
	"net/http"
	"server/internal/config"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	"server/internal/utils"
	"strings"

	"github.com/gin-gonic/gin"
)

func useSecureCookie(c *gin.Context) bool {
	return c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"
}

func setAuthCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(config.AuthCookieName, token, config.AuthTokenExpires*3600, "/", "", useSecureCookie(c), true)
}

func clearAuthCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(config.AuthCookieName, "", -1, "/", "", useSecureCookie(c), true)
}

// AuthToken 用户登录Token拦截（单点登录校验）
func AuthToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		authToken, _ := c.Cookie(config.AuthCookieName)
		if authToken == "" {
			authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
			if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
				authToken = strings.TrimSpace(authHeader[7:])
			} else if authHeader != "" {
				authToken = authHeader
			}
			if authToken == "" {
				authToken = strings.TrimSpace(c.GetHeader("Token"))
			}
		}
		// 如果没有登录信息则直接清退
		if authToken == "" {
			clearAuthCookie(c)
			dto.CustomResult(http.StatusUnauthorized, dto.SUCCESS, nil, "用户未授权,请先登录", c)
			c.Abort()
			return
		}
		// 解析token中的信息
		uc, err := utils.ParseToken(authToken)
		if err != nil || uc == nil {
			clearAuthCookie(c)
			dto.CustomResult(http.StatusUnauthorized, dto.SUCCESS, nil, "身份验证信息已失效,请重新登录!!!", c)
			c.Abort()
			return
		}

		// 单点登录校验：校验当前 Token 是否与 Redis 中最新签发的活跃 Token 一致
		storedToken := repository.GetUserToken(uc.UserID)
		if storedToken == "" {
			clearAuthCookie(c)
			dto.CustomResult(http.StatusUnauthorized, dto.SUCCESS, nil, "身份验证信息已失效,请重新登录!!!", c)
			c.Abort()
			return
		}
		if storedToken != authToken {
			clearAuthCookie(c)
			dto.CustomResult(http.StatusUnauthorized, dto.SUCCESS, nil, "账号在其它设备登录,身份验证信息失效,请重新登录!!!", c)
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
