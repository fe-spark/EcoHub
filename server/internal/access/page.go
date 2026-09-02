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

const pageMinInterval = 300 * time.Millisecond

var (
	pageHitMu   sync.Mutex
	pageHitLast = map[string]time.Time{}
)

func TrackPage(c *gin.Context, action, resource, source, path string) {
	Collect(buildPageEvent(c, action, resource, source, path))
}

func buildPageEvent(c *gin.Context, action, resource, source, path string) *AccessEvent {
	if c == nil || c.Request == nil {
		return nil
	}
	action = strings.ToLower(strings.TrimSpace(action))
	if _, ok := pageActions[action]; !ok {
		return nil
	}
	ua := c.Request.UserAgent()
	if strings.Contains(ua, "EcoHub-SSR") || isCrawlerUA(ua) {
		return nil
	}
	ip := strings.TrimSpace(c.ClientIP())
	if pageTooFast(ip) {
		return nil
	}

	clientType := strings.ToLower(strings.TrimSpace(source))
	if clientType == "android" || clientType == "ohos" || clientType == "ios" {
		clientType = "app"
	}
	if clientType == "" {
		clientType = clientFromUA(ua)
	}

	routePath := strings.TrimSpace(path)
	if routePath == "" {
		routePath = action
	}
	if len(routePath) > maxPathLen {
		routePath = routePath[:maxPathLen]
	}

	return &AccessEvent{
		Ts:         time.Now(),
		Node:       CurrentNodeName(),
		Method:     "PAGE",
		Path:       routePath,
		Route:      "page",
		Action:     action,
		Status:     200,
		ClientType: clientType,
		IPHash:     HashIP(ip),
		IPPreview:  IPPreview(ip),
		UAFamily:   uaFamily("", ua),
		Resource:   TruncateRunes(resource, maxResourceLen),
		playMember: pagePlayRankMember(action, resource),
	}
}

func pageTooFast(ip string) bool {
	ip = strings.TrimSpace(ip)
	if ip == "" || isLoopbackIP(ip) {
		return false
	}
	key := HashIP(ip)
	if key == "" {
		return false
	}
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
