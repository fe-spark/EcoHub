package service

import (
	"strings"

	"server/internal/model"
)

// MergeBannerUpdate 合并单条轮播的编辑结果。
// 编辑弹窗不回传地区、类型、主演、导演等排片时写入的字段，影片没换时这些字段为空就沿用原值。
// 换了影片时以新提交的内容为准，不沿用旧影片的信息。
func MergeBannerUpdate(old, incoming model.Banner) model.Banner {
	merged := incoming
	if merged.Sort == 0 {
		merged.Sort = old.Sort
	}
	if merged.Mid != old.Mid {
		return merged
	}
	keep := func(cur *string, prev string) {
		if strings.TrimSpace(*cur) == "" {
			*cur = prev
		}
	}
	keep(&merged.Area, old.Area)
	keep(&merged.ClassTag, old.ClassTag)
	keep(&merged.Actor, old.Actor)
	keep(&merged.Director, old.Director)
	keep(&merged.Blurb, old.Blurb)
	if merged.Score == 0 {
		merged.Score = old.Score
	}
	if merged.TmdbID == 0 {
		merged.TmdbID = old.TmdbID
	}
	return merged
}
