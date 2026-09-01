package middleware

import (
	"strings"
	"time"

	"server/internal/access"
	"server/internal/config"
	"server/internal/infra/syslog"

	"github.com/gin-gonic/gin"
)

func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		if !config.AccessLogEnabled {
			return
		}
		evt := access.FromContext(c, time.Since(start))
		if evt == nil {
			return
		}
		access.Collect(evt)
		uri := sanitizeAccessLogURI(c.Request.URL.RequestURI())
		if evt.Status >= 400 || evt.LatencyMs >= config.AccessSlowMs {
			syslog.Warnf("[HTTP] %d | %dms | %s | %s %s",
				evt.Status, evt.LatencyMs, evt.IPPreview, evt.Method, uri)
		} else {
			syslog.Infof("[HTTP] %d | %dms | %s | %s %s",
				evt.Status, evt.LatencyMs, evt.IPPreview, evt.Method, uri)
		}
	}
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
