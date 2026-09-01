package access

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
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

type AccessEvent struct {
	Ts         time.Time `json:"ts"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Route      string    `json:"route"`
	Action     string    `json:"action"`
	Status     int       `json:"status"`
	LatencyMs  int64     `json:"latencyMs"`
	ClientType string    `json:"clientType"`
	Internal   string    `json:"internal,omitempty"`
	IPHash     string    `json:"-"`
	IPPreview  string    `json:"ipPreview"`
	UAFamily   string    `json:"uaFamily"`
	Resource   string    `json:"resource"`
	playMember string
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
	path = NormalizePath(path)
	kind := httpKind(path)
	ua := c.Request.UserAgent()
	internal := ""
	if strings.Contains(ua, "EcoHub-SSR") {
		internal = "ssr"
	}
	query := c.Request.URL.Query()
	return &AccessEvent{
		Ts:         time.Now(),
		Method:     method,
		Path:       path,
		Route:      kind,
		Action:     kind,
		Status:     status,
		LatencyMs:  elapsed.Milliseconds(),
		ClientType: ClassifyHTTPClient(path, ua),
		Internal:   internal,
		IPPreview:  IPPreview(c.ClientIP()),
		UAFamily:   uaFamily(path, ua),
		Resource:   httpResource(path, query),
		playMember: playRankMember(path, query),
	}
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

func httpResource(path string, query url.Values) string {
	if strings.HasPrefix(path, "/api/provide/vod") {
		ac := strings.TrimSpace(query.Get("ac"))
		ids := strings.TrimSpace(query.Get("ids"))
		if (ac == "detail" || ac == "videolist") && ids != "" {
			return TruncateRunes(ids, maxResourceLen)
		}
		if wd := strings.TrimSpace(query.Get("wd")); wd != "" {
			return TruncateRunes(wd, maxResourceLen)
		}
		if ac != "" {
			return TruncateRunes(ac, maxResourceLen)
		}
		return "list"
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

func HashIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" || len(config.AccessIPSalt) == 0 {
		return ""
	}
	mac := hmac.New(sha256.New, config.AccessIPSalt)
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
	if evt == nil || evt.Method == "PAGE" || isSSR(evt) {
		return false
	}
	if evt.Status >= 400 || evt.LatencyMs >= config.AccessSlowMs {
		return true
	}
	return httpHealthSample(evt)
}
