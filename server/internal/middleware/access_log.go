package middleware

import (
	"net"
	"net/url"
	"strings"
	"time"

	"server/internal/analytics"
	"server/internal/infra/syslog"

	"github.com/gin-gonic/gin"
)

func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		clientIP := realClientIP(c)
		elapsed := time.Since(start)
		status := c.Writer.Status()

		if analytics.IsEnabled() {
			evt := analytics.FromContext(c, elapsed)
			if evt != nil {
				analytics.Collect(evt)
			}
		}

		if analytics.ShouldSkip(c.Request.Method, c.Request.URL.Path, status) {
			return
		}

		uri := sanitizeAccessLogURI(c.Request.URL.RequestURI())
		latMs := elapsed.Milliseconds()
		ipLog := analytics.IPPreview(clientIP)
		if status >= 400 {
			syslog.Warnf("[HTTP] %d | %dms | %s | %s %s",
				status, latMs, ipLog, c.Request.Method, uri)
		} else {
			syslog.Infof("[HTTP] %d | %dms | %s | %s %s",
				status, latMs, ipLog, c.Request.Method, uri)
		}
	}
}

// realClientIP 解析真实客户端访问 IP。
// 只信任 Gin 基于 TrustedProxies 的 ClientIP，禁止直读客户端可控的 CF-Connecting-IP / X-Real-IP。
func realClientIP(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return "127.0.0.1"
	}
	if clientIP := strings.TrimSpace(c.ClientIP()); clientIP != "" {
		if parsed := net.ParseIP(clientIP); parsed != nil {
			return normalizeIP(clientIP, parsed)
		}
	}
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err == nil && host != "" {
		if parsed := net.ParseIP(host); parsed != nil {
			return normalizeIP(host, parsed)
		}
	}
	return "127.0.0.1"
}

func normalizeIP(raw string, parsed net.IP) string {
	if raw == "::1" || raw == "localhost" || parsed.IsLoopback() {
		return "127.0.0.1"
	}
	// 若为 IPv4 映射地址（::ffff:192.168.1.1），转换为纯净的 IPv4 字符串
	if v4 := parsed.To4(); v4 != nil {
		return v4.String()
	}
	return raw
}

func sanitizeAccessLogURI(uri string) string {
	uri = strings.ReplaceAll(uri, "\n", "")
	uri = strings.ReplaceAll(uri, "\r", "")
	if i := strings.Index(uri, "?"); i >= 0 {
		rawQuery := uri[i+1:]
		if q, err := url.ParseQuery(rawQuery); err == nil {
			q.Del("key")
			if enc := q.Encode(); enc != "" {
				uri = uri[:i] + "?" + enc
			} else {
				uri = uri[:i]
			}
		} else {
			// 查询串含非法百分号转义时按段剔除 key 参数，避免明文订阅密钥落盘。
			parts := strings.Split(rawQuery, "&")
			kept := make([]string, 0, len(parts))
			for _, part := range parts {
				k, _, _ := strings.Cut(part, "=")
				if k == "key" {
					continue
				}
				if part != "" {
					kept = append(kept, part)
				}
			}
			if len(kept) > 0 {
				uri = uri[:i] + "?" + strings.Join(kept, "&")
			} else {
				uri = uri[:i]
			}
		}
	}
	runes := []rune(uri)
	if len(runes) > 512 {
		return string(runes[:512]) + "..."
	}
	return uri
}
