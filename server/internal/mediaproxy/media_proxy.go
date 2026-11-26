package mediaproxy

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"server/internal/model"
	"server/internal/repobase"

	"golang.org/x/sync/singleflight"
)

type mediaProxyRuntime struct {
	config  model.MediaProxyConfig
	siteURL string
}

var (
	cachedRuntime  atomic.Pointer[mediaProxyRuntime]
	runtimeVersion atomic.Uint64
	runtimeSF      singleflight.Group
)

// UpdateMediaProxyRuntime 同步刷新反代策略及本站域名的内存无锁运行时上下文
func UpdateMediaProxyRuntime(cfg model.MediaProxyConfig, rawSiteURL string) {
	runtimeVersion.Add(1)
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
	runtimeVersion.Add(1)
	cachedRuntime.Store(nil)
}

func getActiveRuntime() mediaProxyRuntime {
	if p := cachedRuntime.Load(); p != nil {
		return *p
	}
	val, _, _ := runtimeSF.Do("media-proxy-runtime", func() (any, error) {
		if p := cachedRuntime.Load(); p != nil {
			return *p, nil
		}
		ver := runtimeVersion.Load()
		basic, err := repobase.GetSiteBasic()
		if err != nil {
			return mediaProxyRuntime{config: model.DefaultMediaProxyConfig()}, nil
		}
		norm := strings.TrimRight(strings.ToLower(strings.TrimSpace(basic.SiteURL)), "/")
		rt := mediaProxyRuntime{
			config:  basic.MediaProxy,
			siteURL: norm,
		}
		if runtimeVersion.Load() == ver {
			cachedRuntime.Store(&rt)
		}
		return rt, nil
	})
	return val.(mediaProxyRuntime)
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
	if strings.HasPrefix(trimmed, "data:") ||
		strings.HasPrefix(trimmed, "resource://") ||
		strings.HasPrefix(trimmed, "file://") {
		return trimmed
	}
	if strings.HasPrefix(trimmed, "/") && !strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
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

	if strings.Contains(clean, ".wp.com/") ||
		strings.Contains(clean, "wsrv.nl") ||
		strings.Contains(clean, "statically.io") {
		return trimmed
	}

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

	if rt.siteURL != "" && strings.HasPrefix(strings.ToLower(trimmed), rt.siteURL) {
		return trimmed
	}

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
