package middleware

import (
	"log"
	"net/http"
	"net/url"
	"server/internal/model/dto"
	"strings"

	"github.com/gin-gonic/gin"
)

// Cors 开启跨域请求。带凭证时仅回显与当前请求 Host 一致的 Origin，避免任意源反射。
func Cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		origin := c.Request.Header.Get("Origin")
		if origin != "" && isAllowedCORSOrigin(c, origin) {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE, UPDATE")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Length, X-CSRF-Token, Token,session, Content-Type")
			c.Header("Access-Control-Expose-Headers", "Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers, Content-Type")
			c.Header("Access-Control-Max-Age", "172800")
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Vary", "Origin")
		}

		if method == "OPTIONS" {
			c.JSON(http.StatusOK, "ok!")
			c.Abort()
			return
		}

		defer func() {
			if err := recover(); err != nil {
				log.Printf("Panic info is: %v\n", err)
				if !c.Writer.Written() {
					dto.CustomResult(http.StatusInternalServerError, dto.FAILED, nil, "服务端异常", c)
				}
				c.Abort()
			}
		}()

		c.Next()
	}
}

func isAllowedCORSOrigin(c *gin.Context, origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	reqHost := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if reqHost == "" {
		reqHost = c.Request.Host
	}
	// X-Forwarded-Host 可能带端口列表，只取第一段
	if i := strings.IndexByte(reqHost, ','); i >= 0 {
		reqHost = strings.TrimSpace(reqHost[:i])
	}
	return strings.EqualFold(u.Host, reqHost)
}
