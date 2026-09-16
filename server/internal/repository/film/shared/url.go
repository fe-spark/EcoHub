package shared

import (
	"strings"
)

// StripURLQuery 去掉 URL 的 query 部分，用于图片/播放地址的去重比对。
func StripURLQuery(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i]
	}
	return raw
}
