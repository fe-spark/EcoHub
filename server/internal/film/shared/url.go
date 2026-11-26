package shared

import (
	"strings"

	"server/internal/model"
)

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
