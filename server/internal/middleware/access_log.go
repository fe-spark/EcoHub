package middleware

import (
	"net"
	"strings"
	"time"

	"server/internal/access"
	"server/internal/config"
	"server/internal/infra/syslog"
	"server/internal/model"

	"github.com/gin-gonic/gin"
)

func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		clientIP := realClientIP(c)
		path := c.Request.URL.Path
		elapsed := time.Since(start)
		status := c.Writer.Status()

		if config.AccessLogEnabled {
			evt := access.FromContext(c, elapsed)
			if evt != nil {
				access.Collect(evt)
			}
		}

		// 接口访问记录：排除后台与分析侧噪声（海报、探活、埋点），其余业务 API 入库
		if config.ApiLogEnabled && access.ShouldRecordApiLog(c.Request.Method, path, status) {
			ua := c.Request.UserAgent()
			clientType := access.ClassifyHTTPClient(path, ua)
			access.EnqueueApiAccessLog(&model.ApiAccessLog{
				CreatedAt:  start,
				Method:     access.TruncateRunes(c.Request.Method, 8),
				Path:       access.TruncateRunes(path, 191),
				Query:      access.TruncateRunes(c.Request.URL.RawQuery, 500),
				Status:     status,
				DurationMs: elapsed.Milliseconds(),
				IP:         access.TruncateRunes(clientIP, 45),
				ClientType: access.TruncateRunes(clientType, 16),
				DeviceId:   access.ResolveDeviceID(c, clientType, clientIP, ua),
				UA:         access.TruncateRunes(ua, 255),
			})
		}

		if access.ShouldSkip(c.Request.Method, path, status) {
			return
		}
		uri := sanitizeAccessLogURI(c.Request.URL.RequestURI())
		latMs := elapsed.Milliseconds()
		ipLog := access.IPPreview(clientIP)
		if status >= 400 || latMs >= config.AccessSlowMs {
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
	runes := []rune(uri)
	if len(runes) > 512 {
		return string(runes[:512]) + "..."
	}
	return uri
}
