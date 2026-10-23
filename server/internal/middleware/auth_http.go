package middleware

import (
	"net/http"
	"strings"

	"server/internal/config"

	"github.com/gin-gonic/gin"
)

func useSecureCookie(c *gin.Context) bool {
	return c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"
}

// SetAuthCookie 写入登录令牌。
func SetAuthCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(config.AuthCookieName, token, config.AuthTokenExpires*3600, "/", "", useSecureCookie(c), true)
}

// ClearAuthCookie 清除登录令牌。
func ClearAuthCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(config.AuthCookieName, "", -1, "/", "", useSecureCookie(c), true)
}

// ReadAuthToken 从 Cookie、Authorization 或 Token 头读取登录令牌。
func ReadAuthToken(c *gin.Context) string {
	authToken, _ := c.Cookie(config.AuthCookieName)
	if authToken != "" {
		return authToken
	}
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}
	if authHeader != "" {
		return authHeader
	}
	return strings.TrimSpace(c.GetHeader("Token"))
}
