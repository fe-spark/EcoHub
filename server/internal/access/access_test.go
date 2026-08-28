package access

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"server/internal/config"

	"github.com/gin-gonic/gin"
)

func TestShouldSkip(t *testing.T) {
	cases := []struct {
		method, path string
		status       int
		skip         bool
	}{
		{"GET", "/api/health", 200, true},
		{"HEAD", "/api/health", 200, true},
		{"GET", "/api/index/dailyUpdates", 200, true},
		{"GET", "/api/dailyUpdates", 200, true},
		{"GET", "/api/manage/system/logs/delta", 200, true},
		{"GET", "/api/manage/collect/list", 200, true},
		{"GET", "/api/manage/collect/list", 500, false},
		{"GET", "/api/manage/access/overview", 200, true},
		{"POST", "/api/stat/view", 200, true},
		{"GET", "/api/upload/pic/poster/a.jpg", 200, true},
		{"OPTIONS", "/api/index", 204, true},
		{"GET", "/api/index", 200, false},
		{"GET", "/api/config/basic", 200, false},
		{"GET", "/api/provide/vod", 200, false},
	}
	for _, c := range cases {
		got := ShouldSkip(c.method, c.path, c.status)
		if got != c.skip {
			t.Fatalf("%s %s %d skip=%v want %v", c.method, c.path, c.status, got, c.skip)
		}
	}
}

func TestHTTPKindAndClient(t *testing.T) {
	if httpKind("/api/index") != "http" {
		t.Fatal("index is http not browse")
	}
	if httpKind("/api/searchFilm") != "http" {
		t.Fatal("searchFilm is http not search")
	}
	if httpKind("/api/provide/vod") != "provide" {
		t.Fatal("provide")
	}
	if httpKind("/api/manage/film/search/list") != "manage" {
		t.Fatal("manage")
	}
	if ClassifyHTTPClient("/api/index", "EcoHub-OHOS/1.1.0") != "app" {
		t.Fatal("ohos app")
	}
	if clientFromUA("EcoHub-OHOS/1.1.0") != "app" {
		t.Fatal("page client ua ohos")
	}
	if clientFromUA("EcoHub-iOS/1.0.0") != "app" {
		t.Fatal("page client ua ios")
	}
	if clientFromUA("EcoHub-Android/1.0.0") != "app" {
		t.Fatal("page client ua android")
	}
	if ClassifyHTTPClient("/api/provide/vod", "Mozilla/5.0") != "tvbox" {
		t.Fatal("tvbox by path")
	}
	if ClassifyHTTPClient("/api/index", "curl/8.0") != "crawler" {
		t.Fatal("curl crawler")
	}
	if isCrawlerUA("EcoHub-SSR") {
		t.Fatal("ssr is not crawler")
	}
}

func TestSanitizeAndNormalize(t *testing.T) {
	if SanitizePath("/api/searchFilm?keyword=a\nb") != "/api/searchFilm" {
		t.Fatal("strip query and newline")
	}
	long := "/" + string(make([]byte, 300))
	if len(SanitizePath(long)) != 256 {
		t.Fatal("path max 256")
	}
	if NormalizePath("/api/provide/vod") != "/api/provide/vod" {
		t.Fatal("vod path")
	}
}

func TestHTTPResource(t *testing.T) {
	if httpResource("/api/filmPlayInfo", url.Values{"id": {"12345"}}) != "12345" {
		t.Fatal("filmPlayInfo id")
	}
	if httpResource("/api/provide/vod", url.Values{"ac": {"list"}}) != "list" {
		t.Fatal("provide ac list")
	}
	if httpResource("/api/provide/vod", url.Values{"ac": {"detail"}, "ids": {"999"}}) != "999" {
		t.Fatal("provide ac detail ids")
	}
}

func TestIPPreviewAndHash(t *testing.T) {
	config.AccessIPSalt = []byte("test-salt")
	if IPPreview("203.0.113.45") != "203.0.113.x" {
		t.Fatal("ipv4 preview")
	}
	if IPPreview("127.0.0.1") != "local" {
		t.Fatal("loopback")
	}
	if HashIP("127.0.0.1") == "" {
		t.Fatal("loopback still hashed for uv")
	}
}

func TestRecordRecent(t *testing.T) {
	web := &AccessEvent{Method: "GET", Path: "/api/index", Action: "http", Status: 200}
	if !RecordRecent(web) || !httpHealthSample(web) {
		t.Fatal("public http 2xx in recent and health")
	}
	page := &AccessEvent{Method: "PAGE", Action: "browse", Status: 200}
	if RecordRecent(page) {
		t.Fatal("page view not in http recent")
	}
	ok := &AccessEvent{Action: "provide", Path: "/api/provide/vod", Status: 200, LatencyMs: 20}
	if RecordRecent(ok) || httpHealthSample(ok) {
		t.Fatal("provide 2xx not health/recent")
	}
	admin := &AccessEvent{Action: "manage", Path: "/api/manage/film/search/list", Status: 200}
	if RecordRecent(admin) || httpHealthSample(admin) {
		t.Fatal("manage 2xx not health/recent")
	}
	bad := &AccessEvent{Action: "provide", Path: "/api/provide/vod", Status: 502, LatencyMs: 20}
	if !RecordRecent(bad) {
		t.Fatal("provide error recent")
	}
	ssr := &AccessEvent{
		Method: "GET", Path: "/api/index", Action: "http", Status: 200,
		Internal: "ssr", UAFamily: "ecohub-ssr",
	}
	if RecordRecent(ssr) {
		t.Fatal("ssr 2xx not in recent")
	}
	if !httpHealthSample(ssr) {
		t.Fatal("ssr still in health hist")
	}
	ssrErr := &AccessEvent{
		Method: "GET", Path: "/api/index", Action: "http", Status: 502,
		Internal: "ssr", UAFamily: "ecohub-ssr",
	}
	if RecordRecent(ssrErr) {
		t.Fatal("ssr not in recent even on error")
	}
}

func TestEstimateP95(t *testing.T) {
	if EstimateP95(nil) != 0 {
		t.Fatal("empty")
	}
	if EstimateP95(map[string]int64{"b50": 100}) != 50 {
		t.Fatal("all low")
	}
	got := EstimateP95(map[string]int64{"b50": 90, "b200": 4, "b1000": 6})
	if got != 1000 {
		t.Fatalf("high tail p95=%d", got)
	}
}

func TestUAFamily(t *testing.T) {
	if uaFamily("/api/index", "Mozilla/5.0 Chrome/120.0") != "chrome" {
		t.Fatal("chrome")
	}
	if uaFamily("/api/provide/vod", "okhttp") != "tvbox" {
		t.Fatal("tvbox ua")
	}
	if uaFamily("/api/index", "EcoHub-SSR") != "ecohub-ssr" {
		t.Fatal("ssr ua")
	}
}

func testPageCtx(ua, ip string) *gin.Context {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/api/stat/view", nil)
	req.Header.Set("User-Agent", ua)
	req.RemoteAddr = ip + ":1234"
	c.Request = req
	return c
}

func resetPageHit() {
	pageHitMu.Lock()
	pageHitLast = map[string]time.Time{}
	pageHitMu.Unlock()
}

func TestBuildPageEvent(t *testing.T) {
	config.AccessIPSalt = []byte("test-salt")
	resetPageHit()

	if buildPageEvent(nil, "browse", "", "", "") != nil {
		t.Fatal("nil ctx")
	}
	if buildPageEvent(testPageCtx("Mozilla/5.0", "203.0.113.9"), "click", "", "", "") != nil {
		t.Fatal("unknown action")
	}

	resetPageHit()
	if buildPageEvent(testPageCtx("EcoHub-SSR", "203.0.113.9"), "browse", "", "", "") != nil {
		t.Fatal("ssr skipped")
	}
	resetPageHit()
	if buildPageEvent(testPageCtx("curl/8.0", "203.0.113.9"), "browse", "", "", "") != nil {
		t.Fatal("crawler skipped")
	}

	resetPageHit()
	loop := buildPageEvent(testPageCtx("Mozilla/5.0", "127.0.0.1"), "browse", "", "web", "/")
	if loop == nil || loop.IPHash == "" || loop.IPPreview != "local" || loop.ClientType != "web" || loop.Path != "/" {
		t.Fatal("loopback still hashed")
	}

	resetPageHit()
	long := strings.Repeat("影", maxResourceLen+8)
	play := buildPageEvent(testPageCtx("Mozilla/5.0", "203.0.113.9"), "play", long, "web", "/play?id=1")
	if play == nil || play.Resource != strings.Repeat("影", maxResourceLen) || play.Path != "/play?id=1" {
		t.Fatal("resource truncated")
	}

	resetPageHit()
	first := buildPageEvent(testPageCtx("Mozilla/5.0", "203.0.113.10"), "search", "庆余年", "web", "/search")
	if first == nil {
		t.Fatal("first search")
	}
	if buildPageEvent(testPageCtx("Mozilla/5.0", "203.0.113.10"), "search", "庆余年", "web", "/search") != nil {
		t.Fatal("debounced")
	}
}

func TestShouldSkipHealthOnlyGet(t *testing.T) {
	if !ShouldSkip(http.MethodGet, "/api/health", 200) {
		t.Fatal("get health")
	}
}

func TestFromContextSSR(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/api/index", nil)
	req.Header.Set("User-Agent", "EcoHub-SSR")
	c.Request = req
	evt := FromContext(c, time.Millisecond)
	if evt == nil || evt.Internal != "ssr" || evt.UAFamily != "ecohub-ssr" {
		t.Fatalf("ssr event %+v", evt)
	}
}

func TestEnrichPlayTopItems(t *testing.T) {
	items := []TopItem{
		{Key: "1024", Count: 10},
		{Key: "id 2048", Count: 5},
		{Key: "custom_keyword", Count: 3},
	}
	enriched := enrichPlayTopItems(items)
	if len(enriched) != 3 {
		t.Fatalf("want 3 items, got %d", len(enriched))
	}
	if enriched[0].Title == "" || enriched[1].Title == "" || enriched[2].Title != "custom_keyword" {
		t.Fatalf("enrich items %+v", enriched)
	}
}

