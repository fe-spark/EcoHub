package shared

import (
	"strings"

	"server/internal/model"
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

// HasPlayableLinks 校验影片是否包含至少一条有效播放链接。
func HasPlayableLinks(detail model.MovieDetail) bool {
	for _, group := range detail.PlayList {
		for _, ep := range group {
			if strings.TrimSpace(ep.Link) != "" {
				return true
			}
		}
	}
	return false
}

