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

		// 接口访问记录（仅记录业务与开放 API，/manage 开头的后台请求统统排除）
		if config.ApiLogEnabled && !strings.HasPrefix(path, "/manage") && !strings.HasPrefix(path, "/api/manage") {
			ua := c.Request.UserAgent()
			clientType := access.ClassifyHTTPClient(path, ua)
			access.EnqueueApiAccessLog(&model.ApiAccessLog{
				CreatedAt:  start,
				Method:     c.Request.Method,
				Path:       access.TruncateRunes(path, 191),
				Query:      access.TruncateRunes(c.Request.URL.RawQuery, 500),
				Status:     status,
				DurationMs: elapsed.Milliseconds(),
				IP:         clientIP,
				ClientType: clientType,
				DeviceId:   access.ResolveDeviceID(c, clientType, clientIP, ua),
				UA:         access.TruncateRunes(ua, 255),
			})
		}

		uri := sanitizeAccessLogURI(c.Request.URL.RequestURI())
		latMs := elapsed.Milliseconds()
		if status >= 400 || latMs >= config.AccessSlowMs {
			syslog.Warnf("[HTTP] %d | %dms | %s | %s %s",
				status, latMs, clientIP, c.Request.Method, uri)
		} else {
			syslog.Infof("[HTTP] %d | %dms | %s | %s %s",
				status, latMs, clientIP, c.Request.Method, uri)
		}
	}
}

// realClientIP 解析真实客户端访问 IP
// 针对 Docker 容器网络、K8s Ingress、Nginx/Caddy 反向代理、Cloudflare CDN 及本机访问做精准识别
func realClientIP(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return "127.0.0.1"
	}
	// 1. 优先获取边缘 CDN (如 Cloudflare) 注入的真实客户端 IP
	if cfIP := strings.TrimSpace(c.GetHeader("CF-Connecting-IP")); cfIP != "" {
		if parsed := net.ParseIP(cfIP); parsed != nil {
			return normalizeIP(cfIP, parsed)
		}
	}
	// 2. 优先获取反向代理 (Nginx/Traefik/Ingress) 注入的 X-Real-IP
	if realIP := strings.TrimSpace(c.GetHeader("X-Real-IP")); realIP != "" {
		if parsed := net.ParseIP(realIP); parsed != nil {
			return normalizeIP(realIP, parsed)
		}
	}
	// 3. Gin 框架结合 TrustedProxies 解析的 ClientIP (支持 X-Forwarded-For)
	if clientIP := strings.TrimSpace(c.ClientIP()); clientIP != "" {
		if parsed := net.ParseIP(clientIP); parsed != nil {
			return normalizeIP(clientIP, parsed)
		}
	}
	// 4. TCP 连接 RemoteAddr 保底
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
