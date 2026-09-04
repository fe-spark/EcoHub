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
	"server/internal/model"

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
		{"GET", "/api/manage", 200, true},
		{"GET", "/api/manage/system/logs/delta", 200, true},
		{"GET", "/api/manage/collect/list", 200, true},
		{"GET", "/api/manage/collect/list", 500, true},
		{"GET", "/api/manage/access/overview", 200, true},
		{"POST", "/api/manage/film/add", 200, true},
		{"POST", "/api/stat/view", 200, true},
		{"GET", "/api/upload/pic/poster/a.jpg", 200, true},
		{"OPTIONS", "/api/index", 204, true},
		{"GET", "/api/index", 200, false},
		{"GET", "/api/config/basic", 200, true},
		{"GET", "/api/config/basic", 500, false},
		{"GET", "/api/provide/vod", 200, false},
	}
	for _, c := range cases {
		got := ShouldSkip(c.method, c.path, c.status)
		if got != c.skip {
			t.Fatalf("%s %s %d skip=%v want %v", c.method, c.path, c.status, got, c.skip)
		}
	}
}

func TestShouldRecordApiLog(t *testing.T) {
	cases := []struct {
		method, path string
		status       int
		want         bool
	}{
		{"GET", "/api/index", 200, true},
		{"GET", "/api/provide/vod", 200, false},
		{"GET", "/api/provide/vod", 404, true},
		{"GET", "/api/provide/vod", 500, true},
		{"GET", "/api/config/basic", 500, true},
		{"GET", "/api/health", 200, false},
		{"GET", "/api/config/basic", 200, false},
		{"POST", "/api/stat/view", 200, false},
		{"GET", "/api/upload/pic/poster/a.jpg", 200, false},
		{"GET", "/api/manage/access/overview", 200, false},
		{"GET", "/manage/system", 200, false},
		{"OPTIONS", "/api/index", 204, false},
		{"GET", "", 200, false},
	}
	for _, c := range cases {
		got := ShouldRecordApiLog(c.method, c.path, c.status)
		if got != c.want {
			t.Fatalf("%s %s %d record=%v want %v", c.method, c.path, c.status, got, c.want)
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
	if httpResource("/api/provide/vod", url.Values{"ac": {"list"}}) != "" {
		t.Fatal("provide ac list without t should be empty")
	}
	if httpResource("/api/provide/vod", url.Values{"ac": {"list"}, "t": {"19"}}) != "19" {
		t.Fatal("provide ac list with t")
	}
	if httpResource("/api/provide/vod", url.Values{"ac": {"list"}, "tid": {"19"}}) != "19" {
		t.Fatal("provide ac list with tid")
	}
	if httpResource("/api/provide/vod", url.Values{"ac": {"list"}, "t": {"all"}}) != "" {
		t.Fatal("non-numeric t must not enter classify")
	}
	if httpResource("/api/provide/vod", url.Values{"ac": {"list"}, "t": {"19,20"}}) != "" {
		t.Fatal("comma-separated t must not enter classify")
	}
	if httpResource("/api/provide/vod", url.Values{"ac": {"list"}, "t": {"0"}}) != "" {
		t.Fatal("t=0 must not enter classify")
	}
	if httpResource("/api/provide/vod", url.Values{}) != "" {
		t.Fatal("provide empty ac should be empty")
	}
	if httpResource("/api/provide/vod", url.Values{"ac": {"detail"}, "ids": {"999"}}) != "999" {
		t.Fatal("provide ac detail ids")
	}
	if httpResource("/api/provide/vod", url.Values{"ac": {"detail"}, "t": {"19"}}) != "19" {
		t.Fatal("provide ac detail with t should extract classify ID")
	}
	if httpResource("/api/provide/vod", url.Values{"ac": {"videolist"}, "t": {"19"}}) != "19" {
		t.Fatal("provide ac videolist with t should extract classify ID")
	}
	if httpResource("/api/provide/vod", url.Values{"t": {"19"}}) != "19" {
		t.Fatal("provide bare t should extract classify ID")
	}
	if httpResource("/api/provide/vod", url.Values{"cid": {"19"}}) != "19" {
		t.Fatal("provide cid should extract classify ID")
	}
	if httpResource("/api/provide/vod", url.Values{"ac": {"detail"}, "t": {"19"}, "ids": {"999"}}) != "999" {
		t.Fatal("ids must take priority over t")
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

func TestEnrichClassifyTopItems_FilterDirtyKeys(t *testing.T) {
	input := []TopItem{
		{Key: "list", Count: 65},
		{Key: "config", Count: 12},
		{Key: "19", Count: 53},
		{Key: "34", Count: 11},
	}
	res := enrichClassifyTopItems(input)
	if len(res) != 2 {
		t.Fatalf("enrichClassifyTopItems should filter invalid non-numeric keys like 'list', got len=%d", len(res))
	}
	if res[0].Key != "19" || res[0].Count != 53 {
		t.Fatalf("unexpected res[0]: %+v", res[0])
	}
	if res[1].Key != "34" || res[1].Count != 11 {
		t.Fatalf("unexpected res[1]: %+v", res[1])
	}
	// 在没有数据库连接时，Title 优雅兜底为 分类 #ID
	if res[0].Title != "分类 #19" {
		t.Fatalf("expected Title placeholder '分类 #19', got %s", res[0].Title)
	}
	// 测试截断与过滤结合
	top1 := takeClassifyTops(input, 1)
	if len(top1) != 1 || top1[0].Key != "19" {
		t.Fatalf("takeClassifyTops limit=1 failed: %+v", top1)
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
	if RecordRecent(web) {
		t.Fatal("public http 2xx should NOT be in recent")
	}
	if !httpHealthSample(web) {
		t.Fatal("public http 2xx should be in health")
	}
	page := &AccessEvent{Method: "PAGE", Action: "browse", Status: 200}
	if RecordRecent(page) {
		t.Fatal("page view handled in writePageView, not RecordRecent")
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
	if RecordRecent(bad) {
		t.Fatal("provide error handled in errorKey, not recent")
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
	if evt := FromContext(c, time.Millisecond); evt != nil {
		t.Fatalf("non-provide HTTP must not enter collector, got %+v", evt)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/provide/vod?ac=list", nil)
	req.Header.Set("User-Agent", "EcoHub-SSR")
	c.Request = req
	evt := FromContext(c, time.Millisecond)
	if evt == nil || evt.Internal != "ssr" || evt.UAFamily != "ecohub-ssr" {
		t.Fatalf("ssr provide event %+v", evt)
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
	if info := mk("/api/filmPlayInfo?id=88"); info != nil {
		t.Fatalf("filmPlayInfo HTTP must not enter collector, got %+v", info)
	}
	if keyword := mk("/api/searchFilm?keyword=2024"); keyword != nil {
		t.Fatalf("searchFilm HTTP must not enter collector, got %+v", keyword)
	}
	if idx := mk("/api/index"); idx != nil {
		t.Fatalf("public HTTP must not enter collector, got %+v", idx)
	}

	homeList := mk("/api/provide/vod?ac=list")
	if homeList == nil || homeList.Action != ActionProvide || homeList.Resource != "" {
		t.Fatalf("list without t must stay provide: %+v", homeList)
	}
	classified := mk("/api/provide/vod?ac=list&t=19")
	if classified == nil || classified.Action != ActionClassify || classified.Resource != "19" {
		t.Fatalf("list with t must be classify: %+v", classified)
	}
	detailClassify := mk("/api/provide/vod?ac=detail&t=19")
	if detailClassify == nil || detailClassify.Action != ActionClassify || detailClassify.Resource != "19" {
		t.Fatalf("detail with t must be classify: %+v", detailClassify)
	}
	videolistClassify := mk("/api/provide/vod?ac=videolist&t=19")
	if videolistClassify == nil || videolistClassify.Action != ActionClassify || videolistClassify.Resource != "19" {
		t.Fatalf("videolist with t must be classify: %+v", videolistClassify)
	}
	bareTClassify := mk("/api/provide/vod?t=19")
	if bareTClassify == nil || bareTClassify.Action != ActionClassify || bareTClassify.Resource != "19" {
		t.Fatalf("bare t must be classify: %+v", bareTClassify)
	}
	cidClassify := mk("/api/provide/vod?cid=19")
	if cidClassify == nil || cidClassify.Action != ActionClassify || cidClassify.Resource != "19" {
		t.Fatalf("cid must be classify: %+v", cidClassify)
	}
	dirty := mk("/api/provide/vod?ac=list&t=all")
	if dirty == nil || dirty.Action != ActionProvide || dirty.Resource != "" {
		t.Fatalf("list with dirty t must stay provide: %+v", dirty)
	}
}

func TestQueryLogsMatchStatus(t *testing.T) {
	if !matchStatus("", 200) || !matchStatus("all", 500) {
		t.Fatal("empty or all status matches everything")
	}
	if !matchStatus("2xx", 200) || !matchStatus("2xx", 204) || matchStatus("2xx", 400) {
		t.Fatal("2xx matching")
	}
	if !matchStatus("3xx", 301) || !matchStatus("3xx", 302) || matchStatus("3xx", 200) {
		t.Fatal("3xx matching")
	}
	if !matchStatus("4xx", 404) || matchStatus("4xx", 200) || matchStatus("4xx", 500) {
		t.Fatal("4xx matching")
	}
	if !matchStatus("5xx", 500) || !matchStatus("5xx", 502) || matchStatus("5xx", 200) {
		t.Fatal("5xx matching")
	}
	if !matchStatus("error", 400) || !matchStatus("error", 500) || matchStatus("error", 200) {
		t.Fatal("error matching")
	}
	if !matchStatus("404", 404) || matchStatus("404", 500) || matchStatus("502", 200) {
		t.Fatal("exact status matching")
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
	if !matchQuery("FILMPLAY", evt) {
		t.Fatal("should match path with uppercase query")
	}
	if !matchQuery("  1024  ", evt) {
		t.Fatal("should match resource with untrimmed query")
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
	req, _ := http.NewRequest("GET", "/api/provide/vod?ac=list", nil)
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
	if pageTooFast("", "home") {
		t.Fatal("empty IP must not be throttled")
	}
	// 2. 本地回环不误杀
	if pageTooFast("127.0.0.1", "home") {
		t.Fatal("loopback IP must not be throttled")
	}
	if pageTooFast("::1", "home") {
		t.Fatal("loopback IPv6 must not be throttled")
	}
	// 3. 正常 IP 同页面在防抖窗口内第一次通过，紧接着第二次触发防抖
	testIP := "203.0.113.199"
	first := pageTooFast(testIP, "home")
	if first {
		t.Fatal("first hit should pass")
	}
	second := pageTooFast(testIP, "home")
	if !second {
		t.Fatal("immediate second hit on same page should be throttled")
	}
	// 4. 同 IP 不同页面不应被防抖拦截（规格：同 IP + 同页面防刷）
	diffPage := pageTooFast(testIP, "detail/1024")
	if diffPage {
		t.Fatal("hit on different page should pass")
	}
}

func TestResolveDeviceID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/index", nil)
	c.Request.Header.Set("X-Device-Id", "web-device-1")
	if got := ResolveDeviceID(c, "web", "1.1.1.1", "Mozilla"); got != "web-device-1" {
		t.Fatalf("header device id, got %q", got)
	}

	c.Request = httptest.NewRequest("GET", "/api/film?device_id=q-did", nil)
	if got := ResolveDeviceID(c, "web", "1.1.1.1", "Mozilla"); got != "q-did" {
		t.Fatalf("query device id, got %q", got)
	}

	c.Request = httptest.NewRequest("GET", "/api/provide/vod?ac=list", nil)
	got := ResolveDeviceID(c, "tvbox", "203.0.113.9", "TVBox/1.0")
	if got == "" || !strings.HasPrefix(got, "tv_") {
		t.Fatalf("expected tvbox virtual device id, got %q", got)
	}
}

func TestEventUVIdentity(t *testing.T) {
	withDevice := &AccessEvent{DeviceId: "dev-1", IPHash: "aaa", UAFamily: "chrome", uvMember: "hash-ua"}
	if got := eventUVIdentity(withDevice); got != "hash-ua" {
		t.Fatalf("client device_id must not be UV identity, got %s", got)
	}
	withUA := &AccessEvent{IPHash: "aaa", uvMember: "hash-ip-ua"}
	if got := eventUVIdentity(withUA); got != "hash-ip-ua" {
		t.Fatalf("expected uvMember, got %s", got)
	}
	ipOnly := &AccessEvent{IPHash: "aaa"}
	if got := eventUVIdentity(ipOnly); got != "aaa" {
		t.Fatalf("expected IPHash fallback, got %s", got)
	}
	attacker := &AccessEvent{DeviceId: "eh_did_random_1", IPHash: "same-ip", uvMember: "hash-ip-ua"}
	if got := eventUVIdentity(attacker); got != "hash-ip-ua" {
		t.Fatalf("rotating device_id must not change UV, got %s", got)
	}
}

func TestScopedTopKind(t *testing.T) {
	if got := scopedTopKind("page", "app", "android"); got != "android_page" {
		t.Fatalf("android page kind=%s", got)
	}
	if got := scopedTopKind("play", "app", "harmony"); got != "harmony_play" {
		t.Fatalf("harmony play kind=%s", got)
	}
	if got := scopedTopKind("search", "app", "ios"); got != "ios_search" {
		t.Fatalf("ios search kind=%s", got)
	}
	if got := scopedTopKind("page", "app", "all"); got != "app_page" {
		t.Fatalf("app all page kind=%s", got)
	}
	if got := scopedTopKind("path", "web", ""); got != "web_page" {
		t.Fatalf("web path kind=%s", got)
	}
	if got := scopedTopKind("page", "", ""); got != "page" {
		t.Fatalf("global page kind=%s", got)
	}
	if got := scopedTopKind("path", "", ""); got != "page" {
		t.Fatalf("global path kind=%s", got)
	}
	if got := scopedTopKind("play", "tvbox", ""); got != "tvbox_play" {
		t.Fatalf("tvbox play kind=%s", got)
	}
	if got := scopedTopKind("classify", "app", "android"); got != "android_classify" {
		t.Fatalf("android classify kind=%s", got)
	}
	if got := scopedTopKind("classify", "web", ""); got != "web_classify" {
		t.Fatalf("web classify kind=%s", got)
	}
	if got := scopedTopKind("classify", "tvbox", ""); got != "tvbox_classify" {
		t.Fatalf("tvbox classify kind=%s", got)
	}
	if got := scopedTopKind("classify", "app", "all"); got != "app_classify" {
		t.Fatalf("app all classify kind=%s", got)
	}
	if got := scopedTopKind("classify", "", ""); got != "classify" {
		t.Fatalf("global classify kind=%s", got)
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

func TestBuildPageEventPayload_Platforms(t *testing.T) {
	resetPageHit()
	ctx := testPageCtx("Mozilla/5.0", "192.168.1.100")
	pAndroid := TrackViewPayload{
		Source:      "android",
		Action:      "browse",
		Page:        "HomePage",
		AppVersion:  "1.2.0",
		DeviceModel: "Xiaomi 14",
	}
	evtAndroid := buildPageEventPayload(ctx, pAndroid)
	if evtAndroid == nil || evtAndroid.ClientType != "android" || evtAndroid.Page != "HomePage" || evtAndroid.AppVersion != "1.2.0" {
		t.Fatalf("unexpected android event: %+v", evtAndroid)
	}

	resetPageHit()
	pHarmony := TrackViewPayload{
		Source:      "ohos",
		Action:      "browse",
		Page:        "PlayPage",
		AppVersion:  "1.0.5",
		DeviceModel: "HUAWEI Mate 60",
	}
	evtHarmony := buildPageEventPayload(ctx, pHarmony)
	if evtHarmony == nil || evtHarmony.ClientType != "harmony" || evtHarmony.Page != "PlayPage" {
		t.Fatalf("unexpected harmony event: %+v", evtHarmony)
	}

	resetPageHit()
	pIOS := TrackViewPayload{
		Source:     "ios",
		Action:     "browse",
		Page:       "SearchScreen",
		AppVersion: "2.0.0",
	}
	evtIOS := buildPageEventPayload(ctx, pIOS)
	if evtIOS == nil || evtIOS.ClientType != "ios" || evtIOS.Page != "SearchScreen" {
		t.Fatalf("unexpected ios event: %+v", evtIOS)
	}

	resetPageHit()
	pWeb := TrackViewPayload{
		Source: "web",
		Action: "browse",
		Path:   "/play?id=1024",
	}
	evtWeb := buildPageEventPayload(ctx, pWeb)
	if evtWeb == nil || evtWeb.ClientType != "web" || evtWeb.Page != "/play?id=1024" {
		t.Fatalf("unexpected web event: %+v", evtWeb)
	}

	resetPageHit()
	pWebClassify := TrackViewPayload{
		Source:   "web",
		Action:   "classify",
		Resource: "19",
		Path:     "/filmClassify?Pid=19",
	}
	evtWebClassify := buildPageEventPayload(ctx, pWebClassify)
	if evtWebClassify == nil || evtWebClassify.Action != ActionClassify || evtWebClassify.Resource != "19" || evtWebClassify.ClientType != "web" {
		t.Fatalf("unexpected web classify event: %+v", evtWebClassify)
	}

	resetPageHit()
	pAppClassify := TrackViewPayload{
		Source:   "android",
		Action:   "classify",
		Resource: "19",
		Page:     "FilterPage",
	}
	evtAppClassify := buildPageEventPayload(ctx, pAppClassify)
	if evtAppClassify == nil || evtAppClassify.Action != ActionClassify || evtAppClassify.Resource != "19" || evtAppClassify.ClientType != "android" {
		t.Fatalf("unexpected app classify event: %+v", evtAppClassify)
	}
}

func TestScopeRedisKeys(t *testing.T) {
	day := "20260903"
	if !strings.Contains(webPVKey(day), "web:pv:20260903") {
		t.Fatal("webPVKey")
	}
	if !strings.Contains(appPVKey("android", day), "app:android:pv:20260903") {
		t.Fatal("appPVKey android")
	}
	if !strings.Contains(appPVKey("harmony", day), "app:harmony:pv:20260903") {
		t.Fatal("appPVKey harmony")
	}
	if !strings.Contains(appPVKey("ios", day), "app:ios:pv:20260903") {
		t.Fatal("appPVKey ios")
	}
	if !strings.Contains(appAllPVKey(day), "app:all:pv:20260903") {
		t.Fatal("appAllPVKey")
	}
	if !strings.Contains(appPlatformsKey(day), "app:platforms:20260903") {
		t.Fatal("appPlatformsKey")
	}
	if !strings.Contains(webTopClassifyKey(day), "web:top:classify:20260903") {
		t.Fatal("webTopClassifyKey")
	}
	if !strings.Contains(appTopClassifyKey("android", day), "app:android:top:classify:20260903") {
		t.Fatal("appTopClassifyKey android")
	}
	if !strings.Contains(appAllTopClassifyKey(day), "app:all:top:classify:20260903") {
		t.Fatal("appAllTopClassifyKey")
	}
	if !strings.Contains(tvboxTopClassifyKey(day), "tvbox:top:classify:20260903") {
		t.Fatal("tvboxTopClassifyKey")
	}
}

func TestTruncateRunes_Utf8Safety(t *testing.T) {
	// 测试中文字符不被截成破损字节
	raw := "wd=斗罗大陆双神之战超级无敌好看"
	truncated := TruncateRunes(raw, 5)
	if truncated != "wd=斗罗" {
		t.Fatalf("expected 'wd=斗罗', got %q", truncated)
	}
	if !utf8.ValidString(truncated) {
		t.Fatalf("truncated string is not valid UTF-8: %q", truncated)
	}

	// 边界情况
	if TruncateRunes("", 10) != "" {
		t.Fatal("empty string should return empty")
	}
	if TruncateRunes("abc", 0) != "" {
		t.Fatal("max 0 should return empty")
	}
}

func TestOverviewFromDailyScope_TVBoxSeries(t *testing.T) {
	seriesJSON := `[{"t":"2026-09-02T10:00:00Z","pv":100,"err4":0,"err5":0,"providePv":25}]`
	row := model.AccessDailyStats{
		Day:        "2026-09-02",
		PV:         125,
		UV:         80,
		ProvidePV:  25,
		ProvideUV:  15,
		SeriesJSON: seriesJSON,
	}

	ov := overviewFromDailyScope(row, "tvbox", "")
	if ov.PV != 25 || ov.UV != 15 {
		t.Fatalf("expected TVBox PV=25, UV=15, got PV=%d, UV=%d", ov.PV, ov.UV)
	}
	if len(ov.Series) != 1 || ov.Series[0].PV != 25 {
		t.Fatalf("expected TVBox Series[0].PV=25, got %+v", ov.Series)
	}

	globalOv := overviewFromDailyScope(row, "", "")
	if globalOv.PV != 125 || globalOv.UV != 80 {
		t.Fatalf("expected Global PV=125, UV=80, got PV=%d, UV=%d", globalOv.PV, globalOv.UV)
	}
}

func TestTVBox_FromContextActionAndQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// 1. 测试点播
	c.Request = httptest.NewRequest("GET", "/api/provide/vod?ac=detail&ids=1024", nil)
	evt := FromContext(c, 10*time.Millisecond)
	if evt == nil {
		t.Fatal("expected non-nil evt")
	}
	if evt.Action != ActionPlay {
		t.Fatalf("expected ActionPlay, got %s", evt.Action)
	}
	if evt.Resource != "1024" {
		t.Fatalf("expected Resource 1024, got %s", evt.Resource)
	}
	if evt.Query != "ac=detail&ids=1024" {
		t.Fatalf("expected Query ac=detail&ids=1024, got %s", evt.Query)
	}
	if evt.IPHash == "" {
		t.Fatal("expected non-empty IPHash in HTTP event")
	}
	if evt.IPHash != HashIP(c.ClientIP()) {
		t.Fatalf("expected IPHash %s, got %s", HashIP(c.ClientIP()), evt.IPHash)
	}

	// 2. 测试搜索
	c.Request = httptest.NewRequest("GET", "/api/provide/vod?wd=%E7%BB%88%E6%9E%81%E4%B8%80%E7%8F%AD", nil)
	evt2 := FromContext(c, 10*time.Millisecond)
	if evt2 == nil {
		t.Fatal("expected non-nil evt2")
	}
	if evt2.Action != ActionSearch {
		t.Fatalf("expected ActionSearch, got %s", evt2.Action)
	}
	if evt2.Resource != "终极一班" {
		t.Fatalf("expected Resource 终极一班, got %s", evt2.Resource)
	}
}

func TestOverviewFromDailyScope_WebAndAppSeries(t *testing.T) {
	seriesJSON := `[{"t":"2026-09-02T10:00:00Z","pv":500,"webPv":30,"appPv":20,"androidPv":12,"harmonyPv":3,"iosPv":5,"providePv":50}]`
	row := model.AccessDailyStats{
		Day:            "2026-09-02",
		PV:             500,
		UV:             300,
		WebPV:          30,
		WebUV:          20,
		AppPV:          20,
		AppUV:          15,
		ProvidePV:      50,
		ProvideUV:      35,
		SeriesJSON:     seriesJSON,
		VersionJSON:    `{"1.0.0":10,"1.0.1":10}`,
		PlatformJSON:   `{"android":15,"ios":5}`,
		PlatformUVJSON: `{"android":8,"ios":3}`,
		BrowserJSON:    `{"chrome":25}`,
		OSJSON:         `{"Windows":18,"macOS":7}`,
		ModelsJSON:     `{"Pixel 8":4}`,
	}

	// Web 端验证
	webOv := overviewFromDailyScope(row, "web", "")
	if webOv.PV != 30 || webOv.UV != 20 {
		t.Fatalf("expected Web PV=30, UV=20, got PV=%d, UV=%d", webOv.PV, webOv.UV)
	}
	if len(webOv.Series) != 1 || webOv.Series[0].PV != 30 {
		t.Fatalf("expected Web Series[0].PV=30, got %+v", webOv.Series)
	}

	// App 端验证
	appOv := overviewFromDailyScope(row, "app", "")
	if appOv.PV != 20 || appOv.UV != 15 {
		t.Fatalf("expected App PV=20, UV=15, got PV=%d, UV=%d", appOv.PV, appOv.UV)
	}
	if len(appOv.Series) != 1 || appOv.Series[0].PV != 20 {
		t.Fatalf("expected App Series[0].PV=20, got %+v", appOv.Series)
	}
	if appOv.Versions["1.0.0"] != 10 {
		t.Fatalf("expected Versions[1.0.0]=10, got %+v", appOv.Versions)
	}
	if webOv.Browsers["chrome"] != 25 {
		t.Fatalf("expected Web Browsers[chrome]=25, got %+v", webOv.Browsers)
	}
	if webOv.OS["Windows"] != 18 {
		t.Fatalf("expected Web OS[Windows]=18, got %+v", webOv.OS)
	}

	// App 端分平台下钻验证（Issue 2）
	androidOv := overviewFromDailyScope(row, "app", "android")
	if androidOv.PV != 15 {
		t.Fatalf("expected Android platform PV=15, got %d", androidOv.PV)
	}
	if androidOv.UV != 8 {
		t.Fatalf("expected Android historical UV=8, got %d", androidOv.UV)
	}
	if len(androidOv.Series) != 1 || androidOv.Series[0].PV != 12 {
		t.Fatalf("expected Android Series[0].PV=12, got %+v", androidOv.Series)
	}

	nestedRow := row
	nestedRow.VersionJSON = `{"android":{"2.5.4":7},"ios":{"2.5.4":2}}`
	androidVer := overviewFromDailyScope(nestedRow, "app", "android")
	if androidVer.Versions["2.5.4"] != 7 {
		t.Fatalf("expected android versions 2.5.4=7, got %+v", androidVer.Versions)
	}
	allVer := overviewFromDailyScope(nestedRow, "app", "")
	if allVer.Versions["2.5.4"] != 9 {
		t.Fatalf("expected merged versions 2.5.4=9, got %+v", allVer.Versions)
	}
	if appOv.Models["Pixel 8"] != 4 {
		t.Fatalf("expected Models[Pixel 8]=4, got %+v", appOv.Models)
	}
}

func TestModuleIsolationKeys(t *testing.T) {
	day := "20260903"
	// 验证各端 Top Play Key 完全独立
	wpl := webTopPlayKey(day)
	apl := appAllTopPlayKey(day)
	tvpl := tvboxTopPlayKey(day)
	if wpl == apl || wpl == tvpl || apl == tvpl {
		t.Fatalf("top play keys must be isolated: w=%s a=%s tv=%s", wpl, apl, tvpl)
	}

	// 验证各端 Top Search Key 完全独立
	wsk := webTopSearchKey(day)
	ask := appAllTopSearchKey(day)
	tvsk := tvboxTopSearchKey(day)
	if wsk == ask || wsk == tvsk || ask == tvsk {
		t.Fatalf("top search keys must be isolated: w=%s a=%s tv=%s", wsk, ask, tvsk)
	}

	// 验证各端 Action Key 完全独立
	wak := webActionKey(day)
	aak := appActionKey(day)
	tvak := tvboxActionKey(day)
	if wak == aak || wak == tvak || aak == tvak {
		t.Fatalf("action keys must be isolated: w=%s a=%s tv=%s", wak, aak, tvak)
	}
}
