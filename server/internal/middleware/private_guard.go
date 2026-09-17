package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"server/internal/config"
	"server/internal/model/dto"
	"server/internal/repository"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

var defaultAuthToken = AuthToken()

// PrivateAccessGuard 动态私有化守卫
// 当后台开启「私有化访问」时，前台业务接口必须携带有效登录态或有效的订阅密钥（X-Provide-Key / ?key=）
func PrivateAccessGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := repository.GetSiteBasic()
		if !cfg.PrivateAccess {
			c.Next()
			return
		}

		expectedKey := strings.TrimSpace(cfg.ProvideKey)
		key := strings.TrimSpace(c.Query("key"))
		if key == "" {
			key = strings.TrimSpace(c.GetHeader("X-Provide-Key"))
		}

		// 1. 若携带了正确的 ProvideKey，放行
		if expectedKey != "" && key != "" && subtle.ConstantTimeCompare([]byte(key), []byte(expectedKey)) == 1 {
			c.Next()
			return
		}

		// 2. 若携带了错误 ProvideKey，直接返回私有化密钥错误提示
		if key != "" && expectedKey != "" && subtle.ConstantTimeCompare([]byte(key), []byte(expectedKey)) != 1 {
			dto.CustomResult(http.StatusForbidden, dto.FAILED, gin.H{"private_access": true}, "私有化订阅密钥错误，请核对后重试", c)
			c.Abort()
			return
		}

		// 3. 检查 Web 端用户 Token
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

		if authToken != "" {
			uc, err := utils.ParseToken(authToken)
			if err == nil && uc != nil && uc.ID != "" {
				c.Set("user_id", uc.ID)
				c.Set("role", uc.Role)
				c.Next()
				return
			}
		}

		// 4. 既未提供有效 key，又无有效登录态，明确提示私有化
		dto.CustomResult(http.StatusUnauthorized, dto.FAILED, gin.H{"private_access": true}, "该站点已开启私有化访问，请登录账号或输入订阅密钥", c)
		c.Abort()
	}
}

// ProvideKeyGuard TVBox/影视仓/客户端专属访问密钥校验
// 当开启私有化时：
// 1. 若未配置 ProvideKey，拒绝访问
// 2. 若未带 key，提示需要提供订阅密钥
// 3. 若配置了 ProvideKey，使用恒定时间校验 ?key= 或 Header X-Provide-Key
func ProvideKeyGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := repository.GetSiteBasic()
		if !cfg.PrivateAccess {
			c.Next()
			return
		}

		expectedKey := strings.TrimSpace(cfg.ProvideKey)
		if expectedKey == "" {
			dto.CustomResult(http.StatusForbidden, dto.FAILED, gin.H{"private_access": true}, "该软件源已开启私有化访问，但管理员未设置订阅密钥，请联系管理员", c)
			c.Abort()
			return
		}

		key := strings.TrimSpace(c.Query("key"))
		if key == "" {
			key = strings.TrimSpace(c.GetHeader("X-Provide-Key"))
		}

		if key == "" {
			dto.CustomResult(http.StatusUnauthorized, dto.FAILED, gin.H{"private_access": true}, "该软件源已开启私有化访问，请输入订阅密钥", c)
			c.Abort()
			return
		}

		if subtle.ConstantTimeCompare([]byte(key), []byte(expectedKey)) != 1 {
			dto.CustomResult(http.StatusForbidden, dto.FAILED, gin.H{"private_access": true}, "私有化订阅密钥错误，请核对后重试", c)
			c.Abort()
			return
		}

		c.Next()
	}
}
