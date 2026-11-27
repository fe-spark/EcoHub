package utils

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// maxRetryAfterCooldown 单次 429 冷却上限，防止源站返回异常超大 Retry-After 长时间卡死采集。
	maxRetryAfterCooldown = 60 * time.Second
	retryAfterMarker      = "retry_after="
)

// IsRateLimitedErr 判断错误是否为上游 429 限流响应。
func IsRateLimitedErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "Too Many Requests") || strings.Contains(err.Error(), "status=429")
}

// RateLimitRetryAfter 从限流错误文本中提取上游 Retry-After 建议等待时长；无该信息时返回 0。
func RateLimitRetryAfter(err error) time.Duration {
	if err == nil {
		return 0
	}
	msg := err.Error()
	idx := strings.Index(msg, retryAfterMarker)
	if idx < 0 {
		return 0
	}
	rest := msg[idx+len(retryAfterMarker):]
	end := strings.IndexAny(rest, ", ")
	if end >= 0 {
		rest = rest[:end]
	}
	d, parseErr := time.ParseDuration(strings.TrimSpace(rest))
	if parseErr != nil || d <= 0 {
		return 0
	}
	if d > maxRetryAfterCooldown {
		return maxRetryAfterCooldown
	}
	return d
}

// parseRetryAfterHeader 解析 HTTP Retry-After 头（支持秒数与 HTTP-date），裁剪到安全上限内。
func parseRetryAfterHeader(raw string, now time.Time) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	var d time.Duration
	if sec, err := strconv.Atoi(raw); err == nil {
		if sec <= 0 {
			return 0
		}
		d = time.Duration(sec) * time.Second
	} else if at, err := http.ParseTime(raw); err == nil {
		d = at.Sub(now)
		if d <= 0 {
			return 0
		}
		d = d.Round(time.Second)
		if d < time.Second {
			d = time.Second
		}
	} else {
		return 0
	}
	if d > maxRetryAfterCooldown {
		return maxRetryAfterCooldown
	}
	return d
}
