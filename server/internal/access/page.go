package access

import (
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

var pageActions = map[string]struct{}{
	ActionBrowse:   {},
	ActionSearch:   {},
	ActionPlay:     {},
	ActionClassify: {},
}

const pageMinInterval = 2 * time.Second

var (
	pageHitMu   sync.Mutex
	pageHitLast = map[string]time.Time{}
)

type TrackViewPayload struct {
	Action      string `json:"action"`
	Resource    string `json:"resource"`
	Source      string `json:"source"`
	Path        string `json:"path"`
	Page        string `json:"page"`
	PageTitle   string `json:"page_title"`
	AppVersion  string `json:"app_version"`
	DeviceModel string `json:"device_model"`
	DeviceId    string `json:"device_id"`
}

func TrackPage(c *gin.Context, action, resource, source, path string) {
	Collect(buildPageEventPayload(c, TrackViewPayload{
		Action:   action,
		Resource: resource,
		Source:   source,
		Path:     path,
	}))
}

func TrackPagePayload(c *gin.Context, p TrackViewPayload) {
	Collect(buildPageEventPayload(c, p))
}

func pageClientFromUA(ua string) string {
	switch {
	case strings.Contains(ua, "EcoHub-OHOS"):
		return "harmony"
	case strings.Contains(ua, "EcoHub-iOS") || strings.Contains(ua, "EcoHub-IOS"):
		return "ios"
	case strings.Contains(ua, "EcoHub-Android"):
		return "android"
	default:
		return "web"
	}
}

// isSafePagePath 只放行不会被 <a href> 当作协议解析的页面值。
// page/path 来自公开无鉴权埋点接口，最终可能被管理端渲染为链接：
// 拒绝 "//" 协议相对外链，以及以 scheme 语法（letter…:）开头的值。
func isSafePagePath(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	if strings.HasPrefix(s, "//") {
		return false
	}
	first := s[0]
	if !(first >= 'a' && first <= 'z' || first >= 'A' && first <= 'Z') {
		return true // 不以字母开头，不可能是 URL scheme
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == ':' {
			return false // letter…[:] 即 scheme 前缀，禁止
		}
		if c == '/' || c == '?' || c == '#' {
			return true // 到达 ':' 前先遇到路径分隔符，不是 scheme
		}
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.') {
			return true // 冒号前出现非法 scheme 字符，不是 scheme
		}
	}
	return true // 全程无冒号（如 "HomePage" 屏名），放行
}

func buildPageEvent(c *gin.Context, action, resource, source, path string) *AccessEvent {
	return buildPageEventPayload(c, TrackViewPayload{
		Action:   action,
		Resource: resource,
		Source:   source,
		Path:     path,
	})
}

func buildPageEventPayload(c *gin.Context, p TrackViewPayload) *AccessEvent {
	if c == nil || c.Request == nil {
		return nil
	}
	action := strings.ToLower(strings.TrimSpace(p.Action))
	if _, ok := pageActions[action]; !ok {
		return nil
	}
	ua := c.Request.UserAgent()
	if strings.Contains(ua, "EcoHub-SSR") || isCrawlerUA(ua) {
		return nil
	}
	ip := strings.TrimSpace(c.ClientIP())
	routePath := strings.TrimSpace(p.Path)
	page := strings.TrimSpace(p.Page)
	if page == "" && routePath != "" {
		page = routePath
	}
	if routePath == "" && page != "" {
		routePath = page
	}
	if routePath == "" {
		routePath = action
	}
	routePath = TruncateRunes(routePath, maxPathLen)
	page = TruncateRunes(page, maxPathLen)

	pageKey := page
	if pageKey == "" {
		pageKey = routePath
	}
	if pageTooFast(ip, pageKey) {
		return nil
	}

	clientType := strings.ToLower(strings.TrimSpace(p.Source))
	switch clientType {
	case "android":
		clientType = "android"
	case "ohos", "harmony", "harmonyos":
		clientType = "harmony"
	case "ios":
		clientType = "ios"
	case "web":
		clientType = "web"
	default:
		clientType = pageClientFromUA(ua)
	}

	// Web 端 page/path 会被管理端渲染为链接，只放行不会被当作协议解析的值；
	// 拒绝 "//" 协议相对外链与 scheme 前缀（javascript:/https:/data: 等）。App 屏名不受影响。
	if clientType == "web" && (!isSafePagePath(page) || !isSafePagePath(routePath)) {
		return nil
	}

	did := strings.TrimSpace(p.DeviceId)
	if did == "" {
		did = strings.TrimSpace(c.GetHeader("X-Device-Id"))
	}
	if did == "" {
		did = strings.TrimSpace(c.GetHeader("Device-Id"))
	}

	return &AccessEvent{
		Ts:          time.Now(),
		Node:        CurrentNodeName(),
		Method:      "PAGE",
		Path:        routePath,
		Page:        page,
		PageTitle:   TruncateRunes(p.PageTitle, 64),
		Route:       "page",
		Action:      action,
		Status:      200,
		ClientType:  clientType,
		AppVersion:  TruncateRunes(p.AppVersion, 32),
		DeviceModel: TruncateRunes(p.DeviceModel, 64),
		DeviceId:    TruncateRunes(did, 64),
		IPHash:      HashIP(ip),
		IPPreview:   IPPreview(ip),
		UAFamily:    uaFamily("", ua),
		OS:          detectOS(ua),
		Resource:    TruncateRunes(p.Resource, maxResourceLen),
		playMember:  pagePlayRankMember(action, p.Resource),
		uvMember:    HashIP(ip + "|" + ua),
	}
}

func pageTooFast(ip, pageKey string) bool {
	ip = strings.TrimSpace(ip)
	if ip == "" || isLoopbackIP(ip) {
		return false
	}
	ipHash := HashIP(ip)
	if ipHash == "" {
		return false
	}
	key := ipHash + ":" + strings.TrimSpace(pageKey)
	now := time.Now()
	pageHitMu.Lock()
	defer pageHitMu.Unlock()
	if last, ok := pageHitLast[key]; ok && now.Sub(last) < pageMinInterval {
		return true
	}
	pageHitLast[key] = now
	if len(pageHitLast) > 10000 {
		cutoff := now.Add(-pageMinInterval)
		for k, t := range pageHitLast {
			if t.Before(cutoff) && k != key {
				delete(pageHitLast, k)
			}
		}
		if len(pageHitLast) > 10000 {
			pageHitLast = map[string]time.Time{key: now}
		}
	}
	return false
}
