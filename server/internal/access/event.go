package access

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"server/internal/config"

	"github.com/gin-gonic/gin"
)

const (
	maxPathLen     = 256
	maxResourceLen = 32
	ipHashBytes    = 16
)

const (
	ActionSearch   = "search"
	ActionPlay     = "play"
	ActionBrowse   = "browse"
	ActionClassify = "classify"
	ActionProvide  = "provide"
)

var (
	currentNodeName string
	nodeOnce        sync.Once
)

// CurrentNodeName 获取当前集群节点标识（优先 NODE_NAME -> CLUSTER_ROLE-hostname -> 主机名）
func CurrentNodeName() string {
	nodeOnce.Do(func() {
		hostname, _ := os.Hostname()
		currentNodeName = formatNodeName(config.ClusterRole, hostname, os.Getenv("NODE_NAME"))
	})
	return currentNodeName
}

func formatNodeName(role, hostname, envName string) string {
	if name := strings.TrimSpace(envName); name != "" {
		return name
	}
	runes := []rune(hostname)
	if len(runes) > 12 {
		hostname = string(runes[len(runes)-6:])
	}
	if role == "" {
		role = "node"
	}
	if hostname != "" {
		return role + "-" + hostname
	}
	return role
}

type AccessEvent struct {
	Ts             time.Time `json:"ts"`
	Node           string    `json:"node,omitempty"`
	Method         string    `json:"method"`
	Path           string    `json:"path"`
	Route          string    `json:"route"`
	Action         string    `json:"action"`
	Status         int       `json:"status"`
	LatencyMs      int64     `json:"latencyMs"`
	ClientType     string    `json:"clientType"`
	Internal       string    `json:"internal,omitempty"`
	IPHash         string    `json:"-"`
	IPPreview      string    `json:"ipPreview"`
	UAFamily       string    `json:"uaFamily"`
	Resource       string    `json:"resource"`
	ResourceTitle  string    `json:"resourceTitle,omitempty"`
	ResourcePoster string    `json:"resourcePoster,omitempty"`
	ResourceCat    string    `json:"resourceCat,omitempty"`
	Page           string    `json:"page,omitempty"`
	PageTitle      string    `json:"pageTitle,omitempty"`
	AppVersion     string    `json:"appVersion,omitempty"`
	DeviceModel    string    `json:"deviceModel,omitempty"`
	DeviceId       string    `json:"deviceId,omitempty"`
	OS             string    `json:"os,omitempty"`
	Query          string    `json:"query,omitempty"`
	playMember     string
	uvMember       string
}

func FromContext(c *gin.Context, elapsed time.Duration) *AccessEvent {
	if c == nil || c.Request == nil {
		return nil
	}
	path := SanitizePath(c.Request.URL.Path)
	method := c.Request.Method
	status := c.Writer.Status()
	if ShouldSkip(method, path, status) {
		return nil
	}
	// HTTP 采集只保留 TVBox provide；页面埋点走 TrackPagePayload，避免普通 API 占满采集队列。
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
	action := kind
	if strings.HasPrefix(path, "/api/provide/vod") {
		ac := strings.TrimSpace(query.Get("ac"))
		ids := strings.TrimSpace(query.Get("ids"))
		wd := strings.TrimSpace(query.Get("wd"))
		cid := provideClassifyID(query)

		if ids != "" {
			action = ActionPlay
		} else if wd != "" || ac == "search" {
			action = ActionSearch
		} else if cid != "" {
			action = ActionClassify
		} else if ac == "detail" || ac == "videolist" {
			action = ActionPlay
		} else if ac == "config" || (ac == "" && len(query) == 0) {
			action = "config"
		}
	} else if strings.HasPrefix(path, "/api/provide/config") {
		action = "config"
	}

	clientType := ClassifyHTTPClient(path, ua)
	clientIP := strings.TrimSpace(c.ClientIP())
	did := ResolveDeviceID(c, clientType, clientIP, ua)

	return &AccessEvent{
		Ts:         time.Now(),
		Node:       CurrentNodeName(),
		Method:     method,
		Path:       path,
		Route:      kind,
		Action:     action,
		Status:     status,
		LatencyMs:  elapsed.Milliseconds(),
		ClientType: clientType,
		Internal:   internal,
		IPHash:     HashIP(clientIP),
		IPPreview:  IPPreview(clientIP),
		UAFamily:   uaFamily(path, ua),
		OS:         detectOS(ua),
		Resource:   httpResource(path, query),
		DeviceId:   TruncateRunes(did, 64),
		Query:      TruncateRunes(c.Request.URL.RawQuery, 500),
		playMember: playRankMember(path, query),
		uvMember:   HashIP(clientIP + "|" + ua),
	}
}

// ResolveDeviceID 从 Header / Query 解析客户端设备 ID；TVBox 无上报时用 IP+UA 虚拟指纹。
func ResolveDeviceID(c *gin.Context, clientType, clientIP, ua string) string {
	if c == nil || c.Request == nil {
		return ""
	}
	query := c.Request.URL.Query()
	did := strings.TrimSpace(c.GetHeader("X-Device-Id"))
	if did == "" {
		did = strings.TrimSpace(c.GetHeader("X-Client-Id"))
	}
	if did == "" {
		did = strings.TrimSpace(c.GetHeader("Device-Id"))
	}
	if did == "" {
		did = strings.TrimSpace(query.Get("device_id"))
	}
	if did == "" {
		did = strings.TrimSpace(query.Get("did"))
	}
	if did == "" {
		did = strings.TrimSpace(query.Get("mac"))
	}
	path := c.Request.URL.Path
	if did == "" && (clientType == "tvbox" || strings.HasPrefix(path, "/api/provide/")) {
		did = tvboxVirtualDeviceID(clientIP, ua)
	}
	return TruncateRunes(did, 64)
}

func tvboxVirtualDeviceID(ip, ua string) string {
	raw := strings.TrimSpace(ip) + "|" + strings.TrimSpace(ua)
	sum := md5.Sum([]byte(raw))
	return "tv_" + hex.EncodeToString(sum[:])[:12]
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

func parseFilmID(resource string) (int64, bool) {
	resource = strings.TrimSpace(resource)
	resource = strings.TrimPrefix(resource, "id ")
	id, err := strconv.ParseInt(resource, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func canonicalFilmID(resource string) string {
	id, ok := parseFilmID(resource)
	if !ok {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

func isSingleFilmID(resource string) bool {
	_, ok := parseFilmID(resource)
	return ok
}

func playRankMember(path string, query url.Values) string {
	if strings.HasPrefix(path, "/api/provide/vod") {
		ac := strings.TrimSpace(query.Get("ac"))
		if ac != "detail" && ac != "videolist" {
			return ""
		}
		return canonicalFilmID(query.Get("ids"))
	}
	if strings.HasPrefix(path, "/api/filmPlayInfo") {
		return canonicalFilmID(query.Get("id"))
	}
	return ""
}

func pagePlayRankMember(action, resource string) string {
	if action != ActionPlay {
		return ""
	}
	return canonicalFilmID(resource)
}

func provideClassifyID(query url.Values) string {
	if query == nil {
		return ""
	}
	t := strings.TrimSpace(query.Get("t"))
	if t == "" {
		t = strings.TrimSpace(query.Get("tid"))
	}
	if t == "" {
		t = strings.TrimSpace(query.Get("cid"))
	}
	return canonicalFilmID(t)
}

func httpResource(path string, query url.Values) string {
	if strings.HasPrefix(path, "/api/provide/vod") {
		ac := strings.TrimSpace(query.Get("ac"))
		ids := strings.TrimSpace(query.Get("ids"))
		if ids != "" {
			return TruncateRunes(ids, maxResourceLen)
		}
		if wd := strings.TrimSpace(query.Get("wd")); wd != "" {
			return TruncateRunes(wd, maxResourceLen)
		}
		if cid := provideClassifyID(query); cid != "" {
			return cid
		}
		if ac == "detail" || ac == "videolist" {
			return ""
		}
		return ""
	}
	if strings.HasPrefix(path, "/api/provide/config") {
		return "config"
	}
	if strings.HasPrefix(path, "/api/filmPlayInfo") {
		return TruncateRunes(query.Get("id"), maxResourceLen)
	}
	if strings.HasPrefix(path, "/api/searchFilm") {
		return TruncateRunes(query.Get("keyword"), maxResourceLen)
	}
	return ""
}

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

func eventUVIdentity(evt *AccessEvent) string {
	if evt == nil {
		return ""
	}
	// UV 只使用服务端派生身份；device_id 仅作流水展示，防止客户端枚举刷高独立用户。
	if evt.uvMember != "" {
		return evt.uvMember
	}
	if evt.IPHash != "" && evt.UAFamily != "" {
		return HashIP(evt.IPHash + ":" + evt.UAFamily)
	}
	return evt.IPHash
}

func HashIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	salt := config.AccessIPSalt
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

func IsProvide(evt *AccessEvent) bool {
	return evt != nil && (evt.Action == ActionProvide || isProvidePath(evt.Path))
}

func httpHealthSample(evt *AccessEvent) bool {
	if evt == nil || IsProvide(evt) {
		return false
	}
	return evt.Action != "manage" && !isManagePath(evt.Path)
}

func isSSR(evt *AccessEvent) bool {
	return evt != nil && (evt.Internal == "ssr" || evt.UAFamily == "ecohub-ssr")
}

func RecordRecent(evt *AccessEvent) bool {
	// 普通 HTTP 接口绝不推入 recent 业务访问流水，彻底隔离接口运维日志与业务访问分析
	return false
}
