package ingest

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"server/internal/analytics/config"
	"server/internal/analytics/types"
	appcfg "server/internal/config"

	"github.com/gin-gonic/gin"
)

func TestBuildPageEvent_SecurityAndRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ResetPageHitsForTest()
	t.Cleanup(ResetPageHitsForTest)
	appcfg.AnalyticsIPSalt = []byte("test-salt")

	// 1. 拒绝 javascript: / // 外链路径
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/stats/view", nil)
	c.Request.RemoteAddr = "127.0.0.1:12345"
	if evt := BuildPageEvent(c, types.TrackViewPayload{Action: "browse", Path: "javascript:alert(1)"}); evt != nil {
		t.Fatalf("expected javascript: path rejected, got %+v", evt)
	}
	if evt := BuildPageEvent(c, types.TrackViewPayload{Action: "browse", Path: "//evil.com"}); evt != nil {
		t.Fatalf("expected protocol-relative path rejected, got %+v", evt)
	}

	// 2. 合法请求通过
	evt := BuildPageEvent(c, types.TrackViewPayload{
		Action:        "play",
		Resource:      "123",
		ResourceTitle: "测试电影",
		Path:          "/play?id=123",
		Source:        "web",
	})
	if evt == nil || evt.PlayMember != "123" || evt.ClientType != "web" {
		t.Fatalf("unexpected page event: %+v", evt)
	}

	// 3. 单 IP 窗口防刷限流
	ip := "203.0.113.55"
	for i := 0; i < maxPageEventsPerIPWindow; i++ {
		if PageTooFast(ip, "/film/"+string(rune('a'+i%26))+string(rune('0'+i))) {
			t.Fatalf("request %d should not be throttled", i)
		}
	}
	if !PageTooFast(ip, "/film/overflow") {
		t.Fatal("expected IP window rate limit after overflow")
	}
}

func TestFromContext_ProvideAndCollectorLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	appcfg.AnalyticsIPSalt = []byte("test-salt")
	prev := config.IsEnabled()
	config.SetEnabled(true)
	t.Cleanup(func() { config.SetEnabled(prev) })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/provide/vod?ac=detail&ids=88&key=secret123", nil)
	c.Request.RemoteAddr = "10.0.0.2:9000"

	evt := FromContext(c, 35*time.Millisecond)
	if evt == nil || evt.Action != types.ActionPlay || evt.PlayMember != "88" {
		t.Fatalf("unexpected provide event: %+v", evt)
	}
	if evt.Query == "" || evt.Query == "key=secret123" {
		t.Fatalf("expected key stripped from query: %q", evt.Query)
	}

	StartCollector()
	Collect(evt)
	Shutdown(time.Second)
}
