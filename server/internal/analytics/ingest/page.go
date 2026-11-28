package ingest

import (
	"strings"
	"sync"
	"time"

	"server/internal/analytics/config"
	"server/internal/analytics/enrich"
	"server/internal/analytics/types"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

var pageActions = map[string]struct{}{
	types.ActionBrowse:   {},
	types.ActionSearch:   {},
	types.ActionPlay:     {},
	types.ActionClassify: {},
}

const (
	pageMinInterval          = 2 * time.Second
	pageIPWindow             = 10 * time.Second
	maxPageEventsPerIPWindow = 60
	pageBucketMaxEntries     = 1024
	pageBucketEvictBatch     = pageBucketMaxEntries / 8
	pageHitShardCount        = 16
)

type pageIPWindowState struct {
	windowStart time.Time
	count       int
}

type pageHitShard struct {
	mu       sync.Mutex
	last     map[string]time.Time
	ipWindow map[string]pageIPWindowState
}

var pageHitBuckets [pageHitShardCount]pageHitShard

func init() {
	for i := range pageHitBuckets {
		pageHitBuckets[i].last = map[string]time.Time{}
		pageHitBuckets[i].ipWindow = map[string]pageIPWindowState{}
	}
}

func pageHitBucket(key string) *pageHitShard {
	var hash uint32
	for i := 0; i < len(key); i++ {
		hash = hash*16777619 ^ uint32(key[i])
	}
	return &pageHitBuckets[hash%pageHitShardCount]
}

// ResetPageHitsForTest 清空页面埋点去重窗口（仅限测试）
func ResetPageHitsForTest() {
	for i := range pageHitBuckets {
		bucket := &pageHitBuckets[i]
		bucket.mu.Lock()
		bucket.last = map[string]time.Time{}
		bucket.ipWindow = map[string]pageIPWindowState{}
		bucket.mu.Unlock()
	}
}

// TrackPagePayload 解析并异步入队前端页面埋点事件
func TrackPagePayload(c *gin.Context, p types.TrackViewPayload) {
	if !config.IsEnabled() {
		return
	}
	Collect(BuildPageEvent(c, p))
}

// BuildPageEvent 构建页面埋点事件并执行防刷校验与安全路径过滤
func BuildPageEvent(c *gin.Context, p types.TrackViewPayload) *types.Event {
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
	if PageTooFast(ip, pageKey) {
		return nil
	}

	clientType := normalizePageSource(p.Source, ua)
	if clientType == "web" && (!isSafePagePath(page) || !isSafePagePath(routePath)) {
		return nil
	}

	did := firstNonEmptyTrimmed(p.DeviceId, c.GetHeader("X-Device-Id"), c.GetHeader("Device-Id"))
	ipKey := utils.NormalizeClientIPKey(ip)
	if ipKey == "" {
		ipKey = ip
	}
	var playMember string
	if action == types.ActionPlay {
		playMember = enrich.CanonicalFilmID(p.Resource)
	}
	return &types.Event{
		Ts:              time.Now(),
		Node:            CurrentNodeName(),
		Method:          "PAGE",
		Path:            routePath,
		Page:            page,
		PageTitle:       TruncateRunes(p.PageTitle, 64),
		Route:           "page",
		Action:          action,
		Status:          200,
		ClientType:      clientType,
		AppVersion:      TruncateRunes(p.AppVersion, 32),
		DeviceModel:     TruncateRunes(p.DeviceModel, 64),
		DeviceId:        TruncateRunes(did, 64),
		IPHash:          HashIP(ipKey),
		IPPreview:       IPPreview(ip),
		UAFamily:        uaFamily("", ua),
		OS:              detectOS(ua),
		Resource:        TruncateRunes(p.Resource, maxResourceLen),
		ResourceTitle:   TruncateRunes(p.ResourceTitle, 128),
		ResourcePoster:  enrich.SanitizePosterURL(TruncateRunes(p.ResourcePoster, 512)),
		ResourceCat:     TruncateRunes(p.ResourceCat, 64),
		CollectSourceId: pageCollectSource(action, p.CollectSource, p.Resource),
		PlayMember:      playMember,
		UVMember:        HashIP(ipKey + "|" + ua),
	}
}

func normalizePageSource(source, ua string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "android":
		return "android"
	case "ohos", "harmony", "harmonyos":
		return "harmony"
	case "ios":
		return "ios"
	case "web":
		return "web"
	default:
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
}

// isSafePagePath 拦截 "//" 协议相对外链及 scheme 前缀，避免管理端渲染链接时触发 XSS
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
		return true
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == ':' {
			return false
		}
		if c == '/' || c == '?' || c == '#' {
			return true
		}
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.') {
			return true
		}
	}
	return true
}

// PageTooFast 对单 IP 窗口频率及单页去重间隔执行双层限流
func PageTooFast(ip, pageKey string) bool {
	ip = utils.NormalizeClientIPKey(ip)
	if ip == "" || isLoopbackIP(ip) {
		return false
	}
	ipHash := HashIP(ip)
	if ipHash == "" {
		return false
	}
	now := time.Now()
	ipBucket := pageHitBucket(ipHash)
	ipBucket.mu.Lock()
	win := ipBucket.ipWindow[ipHash]
	if win.windowStart.IsZero() || now.Sub(win.windowStart) >= pageIPWindow {
		win = pageIPWindowState{windowStart: now, count: 0}
	}
	if win.count >= maxPageEventsPerIPWindow {
		ipBucket.mu.Unlock()
		return true
	}
	win.count++
	ipBucket.ipWindow[ipHash] = win
	if len(ipBucket.ipWindow) > pageBucketMaxEntries {
		for k, st := range ipBucket.ipWindow {
			if now.Sub(st.windowStart) >= pageIPWindow && k != ipHash {
				delete(ipBucket.ipWindow, k)
			}
		}
		evictOverflow(ipBucket.ipWindow, ipHash)
	}
	ipBucket.mu.Unlock()

	key := ipHash + ":" + strings.TrimSpace(pageKey)
	bucket := pageHitBucket(key)
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	if last, ok := bucket.last[key]; ok && now.Sub(last) < pageMinInterval {
		return true
	}
	bucket.last[key] = now
	if len(bucket.last) > pageBucketMaxEntries {
		cutoff := now.Add(-pageMinInterval)
		for k, t := range bucket.last {
			if t.Before(cutoff) && k != key {
				delete(bucket.last, k)
			}
		}
		evictOverflow(bucket.last, key)
	}
	return false
}

func evictOverflow[V any](m map[string]V, keepKey string) {
	if len(m) <= pageBucketMaxEntries {
		return
	}
	evict := pageBucketEvictBatch
	for k := range m {
		if evict <= 0 {
			break
		}
		if k != keepKey {
			delete(m, k)
			evict--
		}
	}
}

func firstNonEmptyTrimmed(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
