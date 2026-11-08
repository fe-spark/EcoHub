package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"server/internal/model"
	"server/internal/repository"
)

type mediaProxyRuntime struct {
	config  model.MediaProxyConfig
	siteURL string
}

var cachedRuntime atomic.Pointer[mediaProxyRuntime]

// UpdateMediaProxyRuntime 同步刷新反代策略及本站域名的内存无锁运行时上下文
func UpdateMediaProxyRuntime(cfg model.MediaProxyConfig, rawSiteURL string) {
	norm := strings.TrimRight(strings.ToLower(strings.TrimSpace(rawSiteURL)), "/")
	rt := mediaProxyRuntime{
		config:  cfg,
		siteURL: norm,
	}
	cachedRuntime.Store(&rt)
}

// UpdateMediaProxyMemoryCache 在更新站点配置时同步刷新内存中的反代策略缓存
func UpdateMediaProxyMemoryCache(cfg model.MediaProxyConfig) {
	curr := getActiveRuntime()
	UpdateMediaProxyRuntime(cfg, curr.siteURL)
}

// InvalidateMediaProxyMemoryCache 废弃内存反代缓存
func InvalidateMediaProxyMemoryCache() {
	cachedRuntime.Store(nil)
}

func getActiveRuntime() mediaProxyRuntime {
	if p := cachedRuntime.Load(); p != nil {
		return *p
	}
	basic, err := repository.GetSiteBasic()
	if err != nil {
		rt := mediaProxyRuntime{config: model.DefaultMediaProxyConfig()}
		cachedRuntime.Store(&rt)
		return rt
	}
	norm := strings.TrimRight(strings.ToLower(strings.TrimSpace(basic.SiteURL)), "/")
	rt := mediaProxyRuntime{
		config:  basic.MediaProxy,
		siteURL: norm,
	}
	cachedRuntime.Store(&rt)
	return rt
}

// GetActiveMediaProxyConfig 获取当前有效的媒体反代配置（微秒级内存无锁读取）
func GetActiveMediaProxyConfig() model.MediaProxyConfig {
	return getActiveRuntime().config
}

// ValidateMediaProxyConfig 严格校验媒体反代配置合法性
func ValidateMediaProxyConfig(cfg model.MediaProxyConfig) error {
	tpl := strings.TrimSpace(cfg.Template)
	if tpl == "" {
		return nil
	}
	lower := strings.ToLower(tpl)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return errors.New("反代模板必须以 http:// 或 https:// 开头")
	}
	if !strings.Contains(tpl, "{url}") {
		return errors.New("反代模板必须包含 {url} 占位符")
	}
	return nil
}

// WrapMediaURL 将图片地址根据当前配置转换为公共边缘 CDN 反代地址
func WrapMediaURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	// 本地协议直接返回
	if strings.HasPrefix(trimmed, "data:") ||
		strings.HasPrefix(trimmed, "resource://") ||
		strings.HasPrefix(trimmed, "file://") {
		return trimmed
	}
	// 相对路径（本站自建文件）保持原样
	if strings.HasPrefix(trimmed, "/") && !strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	// 仅对公网 http://, https://, // 处理
	if !strings.HasPrefix(trimmed, "http://") &&
		!strings.HasPrefix(trimmed, "https://") &&
		!strings.HasPrefix(trimmed, "//") {
		return trimmed
	}

	rt := getActiveRuntime()
	if !rt.config.Enabled {
		return trimmed
	}

	var clean string
	if strings.HasPrefix(trimmed, "https://") {
		clean = trimmed[8:]
	} else if strings.HasPrefix(trimmed, "http://") {
		clean = trimmed[7:]
	} else if strings.HasPrefix(trimmed, "//") {
		clean = trimmed[2:]
	} else {
		return trimmed
	}

	// 已接入公共 CDN / 反代，避免二次嵌套
	if strings.Contains(clean, ".wp.com/") ||
		strings.Contains(clean, "wsrv.nl") ||
		strings.Contains(clean, "statically.io") {
		return trimmed
	}

	// 局域网与内网私有地址不走外部公网 CDN
	host := clean
	if slash := strings.Index(host, "/"); slash >= 0 {
		host = host[:slash]
	}
	if colon := strings.Index(host, ":"); colon >= 0 {
		host = host[:colon]
	}
	host = strings.ToLower(host)

	if host == "localhost" ||
		host == "0.0.0.0" ||
		strings.HasPrefix(host, "127.") ||
		strings.HasPrefix(host, "192.168.") ||
		strings.HasPrefix(host, "10.") ||
		strings.HasPrefix(host, "169.254.") {
		return trimmed
	}

	if strings.HasPrefix(host, "172.") {
		parts := strings.Split(host, ".")
		if len(parts) >= 2 {
			if sec, err := strconv.Atoi(parts[1]); err == nil && sec >= 16 && sec <= 31 {
				return trimmed
			}
		}
	}

	// 排除本站域名（纯内存无锁比对，零 I/O 穿透）
	if rt.siteURL != "" && strings.HasPrefix(strings.ToLower(trimmed), rt.siteURL) {
		return trimmed
	}

	// 一致性 Hash 0..2
	var sum int
	for i := 0; i < len(clean); i++ {
		sum = (sum + int(clean[i])) % 3
	}

	tpl := strings.TrimSpace(rt.config.Template)
	if tpl != "" {
		res := strings.ReplaceAll(tpl, "{url}", clean)
		res = strings.ReplaceAll(res, "{hash}", strconv.Itoa(sum))
		return res
	}

	return fmt.Sprintf("https://i%d.wp.com/%s", sum, clean)
}

// WrapMovieBasicInfo 包装单个影片简要信息的封面字段
func WrapMovieBasicInfo(info *model.MovieBasicInfo) {
	if info == nil {
		return
	}
	info.Picture = WrapMediaURL(info.Picture)
	info.PictureSlide = WrapMediaURL(info.PictureSlide)
}

// WrapMovieBasicInfos 批量包装影片简要信息的封面字段（纯函数防御性拷贝，绝不污染源数据）
func WrapMovieBasicInfos(list []model.MovieBasicInfo) []model.MovieBasicInfo {
	if len(list) == 0 {
		return list
	}
	out := make([]model.MovieBasicInfo, len(list))
	copy(out, list)
	for i := range out {
		WrapMovieBasicInfo(&out[i])
	}
	return out
}

// WrapMovieDetail 包装影片详情中的封面字段
func WrapMovieDetail(detail *model.MovieDetail) *model.MovieDetail {
	if detail == nil {
		return nil
	}
	clone := *detail
	clone.Picture = WrapMediaURL(clone.Picture)
	clone.PictureSlide = WrapMediaURL(clone.PictureSlide)
	clone.CustomPicture = WrapMediaURL(clone.CustomPicture)
	clone.CustomPictureSlide = WrapMediaURL(clone.CustomPictureSlide)
	return &clone
}

// WrapMovieDetailVo 包装影片详情 VO 中的封面字段（纯值拷贝，绝不污染原始结构）
func WrapMovieDetailVo(detail model.MovieDetailVo) model.MovieDetailVo {
	detail.Picture = WrapMediaURL(detail.Picture)
	detail.PictureSlide = WrapMediaURL(detail.PictureSlide)
	detail.CustomPicture = WrapMediaURL(detail.CustomPicture)
	detail.CustomPictureSlide = WrapMediaURL(detail.CustomPictureSlide)
	return detail
}

// WrapBanner 包装单条横幅轮播中的海报字段
func WrapBanner(b *model.Banner) {
	if b == nil {
		return
	}
	b.Picture = WrapMediaURL(b.Picture)
	b.Poster = WrapMediaURL(b.Poster)
	b.PictureSlide = WrapMediaURL(b.PictureSlide)
	b.CustomPicture = WrapMediaURL(b.CustomPicture)
}

// WrapBanners 包装横幅轮播列表中的海报字段（纯函数防御性拷贝，绝不污染源数据）
func WrapBanners(banners model.Banners) model.Banners {
	if len(banners) == 0 {
		return banners
	}
	out := make(model.Banners, len(banners))
	copy(out, banners)
	for i := range out {
		WrapBanner(&out[i])
	}
	return out
}

// wrapPictureInMap 防御性包装 map 中代表封面和海报的 URL 字段（纯浅拷贝，绝不污染入参 map）
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
			clone[field] = WrapMediaURL(raw)
		}
	}
	return clone
}

// wrapAnyList 通用包装任意列表类型（原生 struct 切片、[]map[string]any 或 Redis JSON 反序列化产生的 []any）
func wrapAnyList(raw any) any {
	if raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []model.MovieBasicInfo:
		return WrapMovieBasicInfos(v)
	case model.Banners:
		return WrapBanners(v)
	case []model.Banner:
		return WrapBanners(v)
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
				WrapMovieBasicInfo(&info)
				out[i] = info
			} else if b, ok := item.(model.Banner); ok {
				WrapBanner(&b)
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
	clone.List = WrapMovieBasicInfos(res.List)
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
