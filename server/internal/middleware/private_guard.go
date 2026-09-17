package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"server/internal/model/dto"
	"server/internal/repository"

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

		// 若携带了有效的 ProvideKey（原生客户端/播放器订阅软件源），予以放行
		if expectedKey := strings.TrimSpace(cfg.ProvideKey); expectedKey != "" {
			key := strings.TrimSpace(c.Query("key"))
			if key == "" {
				key = strings.TrimSpace(c.GetHeader("X-Provide-Key"))
			}
			if key != "" && subtle.ConstantTimeCompare([]byte(key), []byte(expectedKey)) == 1 {
				c.Next()
				return
			}
		}

		defaultAuthToken(c)
	}
}

// ProvideKeyGuard TVBox/影视仓专属访问密钥校验
// 当开启私有化时：
// 1. 若未配置 ProvideKey，拒绝匿名公网访问
// 2. 若配置了 ProvideKey，使用恒定时间校验 ?key= 或 Header X-Provide-Key
func ProvideKeyGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := repository.GetSiteBasic()
		if !cfg.PrivateAccess {
			c.Next()
			return
		}

		expectedKey := strings.TrimSpace(cfg.ProvideKey)
		if expectedKey == "" {
			dto.CustomResult(http.StatusForbidden, dto.FAILED, nil, "私有化模式下未配置订阅密钥，禁止公网访问", c)
			c.Abort()
			return
		}

		key := strings.TrimSpace(c.Query("key"))
		if key == "" {
			key = strings.TrimSpace(c.GetHeader("X-Provide-Key"))
		}

		if subtle.ConstantTimeCompare([]byte(key), []byte(expectedKey)) != 1 {
			dto.CustomResult(http.StatusForbidden, dto.FAILED, nil, "订阅密钥无效或未授权", c)
			c.Abort()
			return
		}

		c.Next()
	}
}
