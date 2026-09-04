package access

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestIsSafePagePath(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"/play?id=1024", true},
		{"/search?keyword=x", true},
		{"/a/b/c", true},
		{"HomePage", true},
		{"detail/1024", true},
		{"browse", true},
		{"  /safe/path  ", true},
		// scheme / 协议相对注入
		{"javascript:alert(1)", false},
		{"javascript://x", false},
		{"https://evil.com/phish", false},
		{"http://evil.com", false},
		{"//evil.com/x", false},
		{"data:text/html,<script>alert(1)</script>", false},
		{"vbscript:msgbox(1)", false},
		{"mailto:admin@evil.com", false},
	}
	for _, c := range cases {
		if got := isSafePagePath(c.in); got != c.want {
			t.Errorf("isSafePagePath(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestBuildPageEventPayload_RejectsSchemePaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetPageHit()
	ctx := testPageCtx("Mozilla/5.0", "192.168.1.100")

	for _, p := range []string{"javascript:alert(1)", "//evil.com/x", "https://evil.com/phish"} {
		evt := buildPageEventPayload(ctx, TrackViewPayload{
			Action: "browse",
			Source: "web",
			Path:   p,
		})
		if evt != nil {
			t.Fatalf("expected scheme page %q to be dropped, got %+v", p, evt)
		}
	}

	// 合法站内路径与 App 屏名不受影响
	for _, ok := range []struct{ page, path string }{
		{page: "", path: "/play?id=1024"},
		{page: "HomePage", path: ""},
		{page: "SearchScreen", path: ""},
	} {
		evt := buildPageEventPayload(ctx, TrackViewPayload{
			Action: "browse",
			Source: "web",
			Page:   ok.page,
			Path:   ok.path,
		})
		if evt == nil {
			t.Fatalf("expected safe payload page=%q path=%q accepted", ok.page, ok.path)
		}
	}
}

func TestPageClientFromUA(t *testing.T) {
	if got := pageClientFromUA("Mozilla/5.0 (Linux; Android 14; Pixel) Chrome/120"); got != "web" {
		t.Fatalf("mobile browser must stay web, got %q", got)
	}
	if got := pageClientFromUA("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) Safari/604.1"); got != "web" {
		t.Fatalf("iPhone browser must stay web, got %q", got)
	}
	if got := pageClientFromUA("Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) Safari/604.1"); got != "web" {
		t.Fatalf("iPad browser must stay web, got %q", got)
	}
	if got := pageClientFromUA("EcoHub-Android/1.2.0"); got != "android" {
		t.Fatalf("EcoHub-Android got %q", got)
	}
	if got := pageClientFromUA("EcoHub-iOS/2.0.0"); got != "ios" {
		t.Fatalf("EcoHub-iOS got %q", got)
	}
	if got := pageClientFromUA("EcoHub-IOS/2.0.0"); got != "ios" {
		t.Fatalf("EcoHub-IOS got %q", got)
	}
	if got := pageClientFromUA("EcoHub-OHOS/1.0.5"); got != "harmony" {
		t.Fatalf("EcoHub-OHOS got %q", got)
	}
}

func TestBuildPageEventPayload_MobileBrowserWithoutSourceIsWeb(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetPageHit()
	ctx := testPageCtx("Mozilla/5.0 (Linux; Android 14) Chrome/120", "192.168.1.100")
	evt := buildPageEventPayload(ctx, TrackViewPayload{Action: "browse", Path: "/play?id=1"})
	if evt == nil || evt.ClientType != "web" {
		t.Fatalf("mobile web without source must be web, got %+v", evt)
	}

	resetPageHit()
	appCtx := testPageCtx("EcoHub-Android/1.2.0", "192.168.1.101")
	appEvt := buildPageEventPayload(appCtx, TrackViewPayload{Action: "browse", Page: "HomePage"})
	if appEvt == nil || appEvt.ClientType != "android" {
		t.Fatalf("EcoHub-Android without source must be android, got %+v", appEvt)
	}
}
