package middleware

import (
	"log"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var accessLogSkipPaths = map[string]struct{}{
	"/api/manage/collect/list":      {},
	"/api/manage/system/logs/delta": {},
}

func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.RequestURI()
		routePath := c.Request.URL.Path
		c.Next()
		if _, ok := accessLogSkipPaths[routePath]; ok && c.Writer.Status() < 400 {
			return
		}
		log.Printf("[HTTP] %3d | %13s | %15s | %-7s %s", c.Writer.Status(), time.Since(start), c.ClientIP(), c.Request.Method, sanitizeAccessLogPath(path))
	}
}

func sanitizeAccessLogPath(path string) string {
	return strings.ReplaceAll(path, "\n", "")
}
