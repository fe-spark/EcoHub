package portal

import (
	"server/internal/mediaproxy"
	"server/internal/model"
)

func wrapPictureInMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	clone := make(map[string]any, len(m))
	for k, v := range m {
		clone[k] = v
	}
	for _, field := range []string{
		"picture", "Picture",
		"picture_slide", "pictureSlide", "PictureSlide",
		"custom_picture", "customPicture", "CustomPicture",
		"custom_picture_slide", "customPictureSlide", "CustomPictureSlide",
		"poster", "Poster",
	} {
		if raw, ok := clone[field].(string); ok && raw != "" {
			clone[field] = mediaproxy.WrapMediaURL(raw)
		}
	}
	return clone
}

func wrapAnyList(raw any) any {
	if raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []model.MovieBasicInfo:
		return mediaproxy.WrapMovieBasicInfos(v)
	case model.Banners:
		return mediaproxy.WrapBanners(v)
	case []model.Banner:
		return mediaproxy.WrapBanners(v)
	case []map[string]any:
		out := make([]map[string]any, len(v))
		for i := range v {
			out[i] = wrapPictureInMap(v[i])
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			if m, ok := item.(map[string]any); ok {
				out[i] = wrapPictureInMap(m)
			} else if info, ok := item.(model.MovieBasicInfo); ok {
				mediaproxy.WrapMovieBasicInfo(&info)
				out[i] = info
			} else if b, ok := item.(model.Banner); ok {
				mediaproxy.WrapBanner(&b)
				out[i] = b
			} else {
				out[i] = item
			}
		}
		return out
	default:
		return raw
	}
}

// WrapHomeData 包装首页聚合数据中的图片字段（深度兼容结构体切片及 Redis JSON 弱类型切片）
func WrapHomeData(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	out := make(map[string]any, len(data))
	for k, v := range data {
		out[k] = v
	}
	if b, ok := out["banners"]; ok {
		out["banners"] = wrapAnyList(b)
	}
	if content, ok := out["content"]; ok {
		switch c := content.(type) {
		case []map[string]any:
			newContent := make([]map[string]any, len(c))
			for i, sec := range c {
				newSec := make(map[string]any, len(sec))
				for k, v := range sec {
					newSec[k] = v
				}
				if movies, ok := newSec["movies"]; ok {
					newSec["movies"] = wrapAnyList(movies)
				}
				if hot, ok := newSec["hot"]; ok {
					newSec["hot"] = wrapAnyList(hot)
				}
				newContent[i] = newSec
			}
			out["content"] = newContent
		case []any:
			newContent := make([]any, len(c))
			for i, rawSec := range c {
				if sec, ok := rawSec.(map[string]any); ok {
					newSec := make(map[string]any, len(sec))
					for k, v := range sec {
						newSec[k] = v
					}
					if movies, ok := newSec["movies"]; ok {
						newSec["movies"] = wrapAnyList(movies)
					}
					if hot, ok := newSec["hot"]; ok {
						newSec["hot"] = wrapAnyList(hot)
					}
					newContent[i] = newSec
				} else {
					newContent[i] = rawSec
				}
			}
			out["content"] = newContent
		}
	}
	return out
}

// WrapDailyUpdateResult 包装每日更新响应
func WrapDailyUpdateResult(res *DailyUpdateResult) *DailyUpdateResult {
	if res == nil {
		return nil
	}
	clone := *res
	clone.List = mediaproxy.WrapMovieBasicInfos(res.List)
	return &clone
}

// WrapFilmTagSearchResult 包装分类标签筛选结果
func WrapFilmTagSearchResult(resp map[string]any) map[string]any {
	if resp == nil {
		return nil
	}
	out := make(map[string]any, len(resp))
	for k, v := range resp {
		out[k] = v
	}
	if list, ok := out["list"]; ok {
		out["list"] = wrapAnyList(list)
	}
	return out
}

// WrapFilmClassifyContent 包装分类页面聚合内容（深度兼容结构体切片及 Redis JSON 弱类型切片）
func WrapFilmClassifyContent(content map[string]any) map[string]any {
	if content == nil {
		return nil
	}
	out := make(map[string]any, len(content))
	for key, val := range content {
		out[key] = wrapAnyList(val)
	}
	return out
}
