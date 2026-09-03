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
