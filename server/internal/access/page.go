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
	lower := strings.ToLower(ua)
	switch {
	case strings.Contains(ua, "EcoHub-OHOS") || strings.Contains(lower, "openharmony"):
		return "harmony"
	case strings.Contains(ua, "EcoHub-iOS") || strings.Contains(lower, "iphone") || strings.Contains(lower, "ipad"):
		return "ios"
	case strings.Contains(ua, "EcoHub-Android") || strings.Contains(lower, "android"):
		return "android"
	default:
		return "web"
	}
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
