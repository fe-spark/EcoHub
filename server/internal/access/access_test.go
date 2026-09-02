package access

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
	if httpResource("/api/provide/vod", url.Values{}) != "list" {
		t.Fatal("provide empty ac is list")
	}
	if httpResource("/api/provide/vod", url.Values{"ac": {"detail"}, "ids": {"999"}}) != "999" {
		t.Fatal("provide ac detail ids")
	}
	if httpResource("/api/provide/vod", url.Values{"ac": {"videolist"}, "ids": {"888"}}) != "888" {
		t.Fatal("provide videolist ids")
	}
	if httpResource("/api/provide/vod", url.Values{"ac": {"search"}, "wd": {"凡人"}}) != "凡人" {
		t.Fatal("provide wd")
	}
	if httpResource("/api/provide/vod", url.Values{"ac": {"list"}, "wd": {"2024"}}) != "2024" {
		t.Fatal("provide list wd takes priority over ac")
	}
	if httpResource("/api/searchFilm", url.Values{"keyword": {"斗罗"}}) != "斗罗" {
		t.Fatal("searchFilm keyword")
	}
}

func TestIsSingleFilmID(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"123", true},
		{"id 123", true},
		{" 456 ", true},
		{"search", false},
		{"class", false},
		{"list", false},
		{"detail", false},
		{"123,456", false},
		{"0", false},
		{"-1", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isSingleFilmID(tc.input); got != tc.want {
			t.Fatalf("isSingleFilmID(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestPlayRankMember(t *testing.T) {
	cases := []struct {
		path  string
		query url.Values
		want  string
	}{
		{"/api/provide/vod", url.Values{"ac": {"search"}, "wd": {"2024"}}, ""},
		{"/api/provide/vod", url.Values{"ac": {"list"}, "wd": {"1024"}}, ""},
		{"/api/provide/vod", url.Values{"ac": {"search"}, "wd": {"凡人"}}, ""},
		{"/api/provide/vod", url.Values{"ac": {"detail"}, "ids": {"1024"}}, "1024"},
		{"/api/provide/vod", url.Values{"ac": {"videolist"}, "ids": {"88"}}, "88"},
		{"/api/provide/vod", url.Values{"ac": {"detail"}, "ids": {"1,2"}}, ""},
		{"/api/provide/vod", url.Values{"ac": {"detail"}, "ids": {"id 77"}}, "77"},
		{"/api/provide/vod", url.Values{"ac": {"list"}}, ""},
		{"/api/filmPlayInfo", url.Values{"id": {"99"}}, "99"},
		{"/api/filmPlayInfo", url.Values{"id": {"id 88"}}, "88"},
		{"/api/filmPlayInfo", url.Values{"id": {"0"}}, ""},
		{"/api/searchFilm", url.Values{"keyword": {"2024"}}, ""},
	}
	for _, tc := range cases {
		if got := playRankMember(tc.path, tc.query); got != tc.want {
			t.Fatalf("playRankMember(%q, %v) = %q, want %q", tc.path, tc.query, got, tc.want)
		}
	}
}

func TestPagePlayRankMember(t *testing.T) {
	if pagePlayRankMember("play", "2048") != "2048" {
		t.Fatal("numeric play")
	}
	if pagePlayRankMember("play", "id 2048") != "2048" {
		t.Fatal("id prefix play")
	}
	if pagePlayRankMember("play", "庆余年") != "" {
		t.Fatal("title must not rank as play")
	}
	if pagePlayRankMember("search", "2048") != "" {
		t.Fatal("search action must not rank as play")
	}
}

func TestEnrichPlayTopItems_FilterDirtyKeys(t *testing.T) {
	input := []TopItem{
		{Key: "search", Count: 497},
		{Key: "class", Count: 26},
		{Key: "999999", Count: 10},
	}
	res := enrichPlayTopItems(input)
	if len(res) != 1 {
		t.Fatalf("enrichPlayTopItems should filter invalid non-numeric keys, got len=%d", len(res))
	}
	if res[0].Key != "999999" || res[0].Count != 10 {
		t.Fatalf("unexpected res[0]: %+v", res[0])
	}
	if res[0].Title != "影片 #999999" {
		t.Fatalf("expected Title to be placeholder '影片 #999999', got %s", res[0].Title)
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
	if play == nil || play.Resource != strings.Repeat("影", maxResourceLen) || play.Path != "/play?id=1" || play.playMember != "" {
		t.Fatal("resource truncated")
	}

	resetPageHit()
	idPlay := buildPageEvent(testPageCtx("Mozilla/5.0", "203.0.113.11"), "play", "id 2048", "web", "/play?id=2048")
	if idPlay == nil || idPlay.playMember != "2048" {
		t.Fatal("page play member")
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
	if len(enriched) != 2 {
		t.Fatalf("want 2 valid film items (custom_keyword filtered), got %d", len(enriched))
	}
	if enriched[0].Title != "影片 #1024" || enriched[1].Title != "影片 #2048" {
		t.Fatalf("enrich items %+v", enriched)
	}
	if enriched[1].Key != "2048" {
		t.Fatalf("want canonical key 2048, got %q", enriched[1].Key)
	}
}

func TestTakePlayTopsOverFetch(t *testing.T) {
	items := []TopItem{
		{Key: "search", Count: 497},
		{Key: "class", Count: 26},
		{Key: "1", Count: 10},
		{Key: "2", Count: 9},
		{Key: "3", Count: 8},
	}
	got := takePlayTops(items, 2)
	if len(got) != 2 {
		t.Fatalf("want 2 after filter+limit, got %d", len(got))
	}
	if got[0].Key != "1" || got[1].Key != "2" {
		t.Fatalf("unexpected tops %+v", got)
	}
}

func TestFromContextPlayMember(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mk := func(rawURL string) *AccessEvent {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		req := httptest.NewRequest(http.MethodGet, rawURL, nil)
		c.Request = req
		return FromContext(c, time.Millisecond)
	}

	search := mk("/api/provide/vod?ac=search&wd=2024")
	if search == nil || search.Resource != "2024" || search.playMember != "" {
		t.Fatalf("numeric search must not rank as play: %+v member=%q", search, search.playMember)
	}
	detail := mk("/api/provide/vod?ac=detail&ids=1024")
	if detail == nil || detail.playMember != "1024" {
		t.Fatalf("detail ids should rank as play: %+v", detail)
	}
	info := mk("/api/filmPlayInfo?id=88")
	if info == nil || info.playMember != "88" {
		t.Fatalf("filmPlayInfo should rank as play: %+v", info)
	}
	keyword := mk("/api/searchFilm?keyword=2024")
	if keyword == nil || keyword.Resource != "2024" || keyword.playMember != "" {
		t.Fatalf("searchFilm must not rank as play: %+v member=%q", keyword, keyword.playMember)
	}
}

func TestQueryLogsMatchStatus(t *testing.T) {
	if !matchStatus("", 200) || !matchStatus("all", 500) {
		t.Fatal("empty or all status matches everything")
	}
	if !matchStatus("2xx", 200) || !matchStatus("2xx", 204) || matchStatus("2xx", 400) {
		t.Fatal("2xx matching")
	}
	if !matchStatus("4xx", 404) || matchStatus("4xx", 200) || matchStatus("4xx", 500) {
		t.Fatal("4xx matching")
	}
	if !matchStatus("5xx", 500) || !matchStatus("5xx", 502) || matchStatus("5xx", 200) {
		t.Fatal("5xx matching")
	}
}

func TestQueryLogsMatchQuery(t *testing.T) {
	evt := &AccessEvent{
		Path:      "/api/filmPlayInfo",
		IPPreview: "192.168.1.x",
		Resource:  "1024",
	}

	if !matchQuery("", evt) {
		t.Fatal("empty query should match all")
	}
	if !matchQuery("192.168", evt) {
		t.Fatal("should match IP prefix")
	}
	if !matchQuery("1.x", evt) {
		t.Fatal("should match IP preview suffix")
	}
	if !matchQuery("filmplay", evt) {
		t.Fatal("should match path case-insensitively")
	}
	if !matchQuery("1024", evt) {
		t.Fatal("should match resource")
	}
	if matchQuery("10.0.0", evt) {
		t.Fatal("should not match unrelated IP")
	}
	if matchQuery("search", evt) {
		t.Fatal("should not match unrelated path/keyword")
	}
}

func TestCurrentNodeName(t *testing.T) {
	node := CurrentNodeName()
	if node == "" {
		t.Fatal("node name should not be empty")
	}
	// 验证在 AccessEvent 中携带 node
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest("GET", "/api/index", nil)
	c.Request = req
	evt := FromContext(c, 5*time.Millisecond)
	if evt == nil {
		t.Fatal("evt should not be nil")
	}
	if evt.Node != node {
		t.Fatalf("evt.Node=%q want %q", evt.Node, node)
	}
}

func TestPageTooFastSafety(t *testing.T) {
	// 1. 空 IP 不误杀
	if pageTooFast("") {
		t.Fatal("empty IP must not be throttled")
	}
	// 2. 本地回环不误杀
	if pageTooFast("127.0.0.1") {
		t.Fatal("loopback IP must not be throttled")
	}
	if pageTooFast("::1") {
		t.Fatal("loopback IPv6 must not be throttled")
	}
	// 3. 正常 IP 在防抖窗口内第一次通过，紧接着第二次触发防抖
	testIP := "203.0.113.199"
	first := pageTooFast(testIP)
	if first {
		t.Fatal("first hit should pass")
	}
	second := pageTooFast(testIP)
	if !second {
		t.Fatal("immediate second hit should be throttled")
	}
}

func TestDroppedKeys(t *testing.T) {
	globalKey := droppedKey()
	dayKey := droppedDayKey("20260902")
	lockKey := rollupLockKey()
	if !strings.Contains(globalKey, "meta:dropped") {
		t.Fatalf("unexpected globalKey: %s", globalKey)
	}
	if !strings.Contains(dayKey, "meta:dropped:20260902") {
		t.Fatalf("unexpected dayKey: %s", dayKey)
	}
	if !strings.Contains(lockKey, "lock:daily_rollup") {
		t.Fatalf("unexpected lockKey: %s", lockKey)
	}
}

func TestFormatNodeName(t *testing.T) {
	if got := formatNodeName("master", "myhost", "custom-node-1"); got != "custom-node-1" {
		t.Fatalf("want custom-node-1, got %q", got)
	}
	if got := formatNodeName("worker", "myhost", ""); got != "worker-myhost" {
		t.Fatalf("want worker-myhost, got %q", got)
	}
	// >12 字符截取后 6 个字符
	if got := formatNodeName("node", "ecohub-cluster-node-99", ""); got != "node-ode-99" {
		t.Fatalf("want node-ode-99, got %q", got)
	}
	// UTF-8 非 ASCII 字符不截断乱码
	utf8Host := "中文开发环境-测试节点-01"
	got := formatNodeName("master", utf8Host, "")
	if !utf8.ValidString(got) {
		t.Fatalf("formatNodeName produced invalid UTF-8: %q", got)
	}
	if got != "master-试节点-01" {
		t.Fatalf("want master-试节点-01, got %q", got)
	}
}
