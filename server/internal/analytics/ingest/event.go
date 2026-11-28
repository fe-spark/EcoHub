package ingest

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"server/internal/analytics/enrich"
	"server/internal/analytics/types"
	appcfg "server/internal/config"
	"server/internal/sourcecatalog"

	"github.com/gin-gonic/gin"
)

const (
	maxPathLen     = 256
	maxResourceLen = 32
	ipHashBytes    = 16
)

var (
	currentNodeName string
	nodeOnce        sync.Once

	lookupActiveSourceID = func() string {
		active := sourcecatalog.CurrentPrimarySource()
		if active == nil {
			return ""
		}
		return strings.TrimSpace(active.Id)
	}
	lookupSourceExists = func(id string) bool {
		return sourcecatalog.HasCollectSource(id)
	}
)

// CurrentNodeName 获取当前节点标识（优先 NODE_NAME，否则截断主机名）
func CurrentNodeName() string {
	nodeOnce.Do(func() {
		hostname, _ := os.Hostname()
		currentNodeName = formatNodeName(hostname, os.Getenv("NODE_NAME"))
	})
	return currentNodeName
}

func formatNodeName(hostname, envName string) string {
	if name := strings.TrimSpace(envName); name != "" {
		return name
	}
	runes := []rune(hostname)
	if len(runes) > 12 {
		hostname = string(runes[len(runes)-6:])
	}
	if hostname != "" {
		return hostname
	}
	return "ecohub"
}

// FromContext 从 HTTP 请求上下文构建访问事件（当前仅采集 TVBox /api/provide/* 请求）
func FromContext(c *gin.Context, elapsed time.Duration) *types.Event {
	if c == nil || c.Request == nil {
		return nil
	}
	path := SanitizePath(c.Request.URL.Path)
	if !isProvidePath(path) {
		return nil
	}
	path = NormalizePath(path)
	kind := httpKind(path)
	ua := c.Request.UserAgent()
	internal := ""
	if strings.Contains(ua, "EcoHub-SSR") {
		internal = "ssr"
	}
	query := c.Request.URL.Query()
	action := resolveProvideAction(path, query)

	clientType := ClassifyHTTPClient(path, ua)
	clientIP := strings.TrimSpace(c.ClientIP())
	did := ResolveDeviceID(c, clientType, clientIP, ua)

	return &types.Event{
		Ts:              time.Now(),
		Node:            CurrentNodeName(),
		Method:          c.Request.Method,
		Path:            path,
		Route:           kind,
		Action:          action,
		Status:          c.Writer.Status(),
		LatencyMs:       elapsed.Milliseconds(),
		ClientType:      clientType,
		Internal:        internal,
		IPHash:          HashIP(clientIP),
		IPPreview:       IPPreview(clientIP),
		UAFamily:        uaFamily(path, ua),
		OS:              detectOS(ua),
		Resource:        httpResource(path, query),
		DeviceId:        TruncateRunes(did, 64),
		Query:           TruncateRunes(sanitizeQuery(query), 500),
		CollectSourceId: provideCollectSource(path),
		PlayMember:      playRankMember(path, query),
		UVMember:        HashIP(clientIP + "|" + ua),
	}
}

func resolveProvideAction(path string, query url.Values) string {
	if strings.HasPrefix(path, "/api/provide/config") {
		return types.ActionConfig
	}
	if !strings.HasPrefix(path, "/api/provide/vod") {
		return types.ActionProvide
	}
	ac := strings.TrimSpace(query.Get("ac"))
	ids := strings.TrimSpace(query.Get("ids"))
	wd := strings.TrimSpace(query.Get("wd"))
	cid := provideClassifyID(query)
	switch {
	case ids != "":
		return types.ActionPlay
	case wd != "" || ac == "search":
		return types.ActionSearch
	case cid != "":
		return types.ActionClassify
	case ac == "detail" || ac == "videolist":
		return types.ActionPlay
	case ac == "config" || (ac == "" && len(query) == 0):
		return types.ActionConfig
	default:
		return types.ActionProvide
	}
}

func sanitizeQuery(query url.Values) string {
	if len(query) == 0 {
		return ""
	}
	copied := make(url.Values, len(query))
	for key, values := range query {
		if strings.EqualFold(key, "key") {
			continue
		}
		copied[key] = values
	}
	return copied.Encode()
}

// ResolveDeviceID 从请求头或参数解析设备 ID；TVBox 无上报时派生虚拟指纹
func ResolveDeviceID(c *gin.Context, clientType, clientIP, ua string) string {
	if c == nil || c.Request == nil {
		return ""
	}
	query := c.Request.URL.Query()
	candidates := []string{
		c.GetHeader("X-Device-Id"),
		c.GetHeader("X-Client-Id"),
		c.GetHeader("Device-Id"),
		query.Get("device_id"),
		query.Get("did"),
		query.Get("mac"),
	}
	var did string
	for _, item := range candidates {
		if v := strings.TrimSpace(item); v != "" {
			did = v
			break
		}
	}
	if did == "" && (clientType == "tvbox" || strings.HasPrefix(c.Request.URL.Path, "/api/provide/")) {
		raw := strings.TrimSpace(clientIP) + "|" + strings.TrimSpace(ua)
		sum := md5.Sum([]byte(raw))
		did = "tv_" + hex.EncodeToString(sum[:])[:12]
	}
	return TruncateRunes(did, 64)
}

func SanitizePath(raw string) string {
	raw = strings.ReplaceAll(raw, "\n", "")
	raw = strings.ReplaceAll(raw, "\r", "")
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "/"
	}
	if len(raw) > maxPathLen {
		raw = raw[:maxPathLen]
	}
	return raw
}

func NormalizePath(path string) string {
	if strings.HasPrefix(path, "/api/provide/vod") {
		return "/api/provide/vod"
	}
	if strings.HasPrefix(path, "/api/provide/config") {
		return "/api/provide/config"
	}
	return path
}

func TruncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" || max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

func playRankMember(path string, query url.Values) string {
	if strings.HasPrefix(path, "/api/provide/vod") {
		ac := strings.TrimSpace(query.Get("ac"))
		if ac != "detail" && ac != "videolist" {
			return ""
		}
		return enrich.CanonicalFilmID(query.Get("ids"))
	}
	if strings.HasPrefix(path, "/api/films/play-info") {
		return enrich.CanonicalFilmID(query.Get("id"))
	}
	return ""
}

func provideClassifyID(query url.Values) string {
	if query == nil {
		return ""
	}
	for _, k := range []string{"t", "tid", "cid"} {
		if v := strings.TrimSpace(query.Get(k)); v != "" {
			return enrich.CanonicalFilmID(v)
		}
	}
	return ""
}

func httpResource(path string, query url.Values) string {
	if strings.HasPrefix(path, "/api/provide/vod") {
		if ids := strings.TrimSpace(query.Get("ids")); ids != "" {
			return TruncateRunes(ids, maxResourceLen)
		}
		if wd := strings.TrimSpace(query.Get("wd")); wd != "" {
			return TruncateRunes(wd, maxResourceLen)
		}
		if cid := provideClassifyID(query); cid != "" {
			return cid
		}
		return ""
	}
	if strings.HasPrefix(path, "/api/provide/config") {
		return "config"
	}
	if strings.HasPrefix(path, "/api/films/play-info") {
		return TruncateRunes(query.Get("id"), maxResourceLen)
	}
	if strings.HasPrefix(path, "/api/films/search") {
		return TruncateRunes(query.Get("keyword"), maxResourceLen)
	}
	return ""
}

// ValidCollectSourceID 校验采集站 ID 合法性
func ValidCollectSourceID(id string) bool {
	if id == "" || len(id) > 32 {
		return false
	}
	for _, r := range id {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func activeCollectSourceID() string {
	id := strings.TrimSpace(lookupActiveSourceID())
	if !ValidCollectSourceID(id) {
		return ""
	}
	return id
}

func provideCollectSource(path string) string {
	if !strings.HasPrefix(path, "/api/provide/vod") {
		return ""
	}
	return activeCollectSourceID()
}

func pageCollectSource(action, collectSource, resource string) string {
	if action != types.ActionPlay {
		return ""
	}
	if id := strings.TrimSpace(collectSource); id != "" {
		if ValidCollectSourceID(id) && lookupSourceExists(id) {
			return id
		}
		return ""
	}
	idx := strings.LastIndex(strings.TrimSpace(resource), ":")
	if idx > 0 && idx < len(strings.TrimSpace(resource))-1 {
		liveID := strings.TrimSpace(strings.TrimSpace(resource)[:idx])
		if ValidCollectSourceID(liveID) && lookupSourceExists(liveID) {
			return liveID
		}
		return ""
	}
	return activeCollectSourceID()
}

// IPPreview 生成脱敏展示 IP
func IPPreview(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" || isLoopbackIP(ip) {
		return "local"
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "local"
	}
	if v4 := parsed.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.x", v4[0], v4[1], v4[2])
	}
	mask := make(net.IP, net.IPv6len)
	copy(mask, parsed.To16())
	for i := 6; i < net.IPv6len; i++ {
		mask[i] = 0
	}
	return mask.String()
}

// HashIP 使用服务端密钥对 IP 进行单向哈希，用于去重与 UV 统计
func HashIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	salt := appcfg.AnalyticsIPSalt
	if len(salt) == 0 {
		salt = []byte("ecohub-access-ip-salt-default")
	}
	mac := hmac.New(sha256.New, salt)
	_, _ = mac.Write([]byte(ip))
	sum := mac.Sum(nil)
	if len(sum) > ipHashBytes {
		sum = sum[:ipHashBytes]
	}
	return hex.EncodeToString(sum)
}

func isLoopbackIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ip == "localhost"
	}
	return parsed.IsLoopback()
}
