package admin

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net/http"
	"net/http/httptest"
	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/middleware"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repobase"
	"server/internal/testutil"
	"server/internal/utils"
	"strings"
	"testing"
)

func TestSiteBasicAndDanmakuConfig_RedactsDanmakuApiURLForNonAdmin(t *testing.T) {
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.SiteConfigRecord{}); err != nil {
		t.Fatal(err)
	}
	prevDB, prevRdb := db.Mdb, db.Rdb
	db.Mdb, db.Rdb = gdb, nil
	t.Cleanup(func() { db.Mdb, db.Rdb = prevDB, prevRdb })

	cfg := model.BasicConfig{
		SiteName:   "TestSite",
		ProvideKey: "prov-secret",
		Danmaku: model.DanmakuConfig{
			Enabled:   true,
			AutoSniff: true,
			ApiURL:    "https://danmu.example.com/my-secret-token/api/v2",
			AppID:     "appid-1",
			AppSecret: "appsec-1",
		},
	}
	if err := repobase.SaveSiteBasic(cfg); err != nil {
		t.Fatal(err)
	}

	// 1. 匿名请求 SiteBasicConfig 不得暴露 ApiURL/AppID/AppSecret/ProvideKey
	c, w := testutil.NewTestContext(http.MethodGet, "/api/config/basic")
	ManageHd.SiteBasicConfig(c)
	resp := testutil.DecodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected SUCCESS, got %d", resp.Code)
	}
	raw, _ := json.Marshal(resp.Data)
	body := string(raw)
	for _, secret := range []string{"my-secret-token", "appid-1", "appsec-1", "prov-secret"} {
		if strings.Contains(body, secret) {
			t.Fatalf("anonymous SiteBasicConfig leaked %q in %s", secret, body)
		}
	}

	// 2. 访客（非超管）请求 SiteDanmakuConfig 不得暴露 ApiURL
	c2, w2 := testutil.NewTestContext(http.MethodGet, "/api/manage/config/danmaku")
	c2.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 20001, Role: model.UserRoleVisitor})
	ManageHd.SiteDanmakuConfig(c2)
	resp2 := testutil.DecodeResponse(t, w2)
	raw2, _ := json.Marshal(resp2.Data)
	if strings.Contains(string(raw2), "my-secret-token") {
		t.Fatalf("non-admin SiteDanmakuConfig leaked ApiURL: %s", string(raw2))
	}

	// 3. 超管可以看到完整配置
	c3, w3 := testutil.NewTestContext(http.MethodGet, "/api/manage/config/danmaku")
	c3.Set(config.AuthUserClaims, &utils.UserClaims{UserID: config.UserIdInitialVal, Role: model.UserRoleAdmin})
	ManageHd.SiteDanmakuConfig(c3)
	resp3 := testutil.DecodeResponse(t, w3)
	raw3, _ := json.Marshal(resp3.Data)
	if !strings.Contains(string(raw3), "my-secret-token") {
		t.Fatalf("admin SiteDanmakuConfig should retain ApiURL, got %s", string(raw3))
	}

	// 4. 开启私有化时，匿名请求 SiteBasicConfig 应额外隐去公告/赞赏/站点链接/描述
	privateCfg := model.BasicConfig{
		SiteName:      "PrivateSite",
		SiteURL:       "https://private.example.com",
		Describe:      "Secret Description",
		Keyword:       "secret-kw",
		PrivateAccess: true,
		ProvideKey:    "prov-secret-1234",
		Tip:           model.TipConfig{Enabled: true, Title: "私密打赏", Message: "Secret Tip"},
		Notice:        model.NoticeConfig{Enabled: true, Title: "内部公告", Content: "Secret Notice"},
		MediaProxy:    model.MediaProxyConfig{Enabled: true, Template: "https://worker.secret/{url}"},
	}
	if err := repobase.SaveSiteBasic(privateCfg); err != nil {
		t.Fatal(err)
	}
	c4, w4 := testutil.NewTestContext(http.MethodGet, "/api/config/basic")
	ManageHd.SiteBasicConfig(c4)
	resp4 := testutil.DecodeResponse(t, w4)
	raw4, _ := json.Marshal(resp4.Data)
	body4 := string(raw4)
	for _, hidden := range []string{"Secret Description", "secret-kw", "Secret Tip", "Secret Notice", "worker.secret", "private.example.com", "prov-secret-1234"} {
		if strings.Contains(body4, hidden) {
			t.Fatalf("anonymous private SiteBasicConfig leaked %q in %s", hidden, body4)
		}
	}
	if !strings.Contains(body4, `"privateAccess":true`) || !strings.Contains(body4, "PrivateSite") {
		t.Fatalf("expected privateAccess and siteName preserved, got %s", body4)
	}

	// 5. 携带有效 X-Provide-Key 请求 SiteBasicConfig 应返回公告等展示信息，仍隐藏 ProvideKey
	c5, w5 := testutil.NewTestContext(http.MethodGet, "/api/config/basic")
	c5.Request.RemoteAddr = "198.51.100.76:12345"
	c5.Request.Header.Set("X-Provide-Key", "prov-secret-1234")
	ManageHd.SiteBasicConfig(c5)
	resp5 := testutil.DecodeResponse(t, w5)
	raw5, _ := json.Marshal(resp5.Data)
	body5 := string(raw5)
	if !strings.Contains(body5, "Secret Notice") || strings.Contains(body5, "prov-secret-1234") {
		t.Fatalf("expected Notice visible and ProvideKey hidden for authorized client, got %s", body5)
	}

	// 6. 同一 IP 连续尝试错误 key 触发限流后，SiteBasicConfig 即使携带正确 key 也只能拿到脱敏配置
	for i := 0; i < 10; i++ {
		cb, wb := testutil.NewTestContext(http.MethodGet, "/api/config/basic?key=wrong-guess")
		cb.Request.RemoteAddr = "198.51.100.77:12345"
		ManageHd.SiteBasicConfig(cb)
		if wb.Code != http.StatusOK {
			t.Fatalf("unexpected status %d", wb.Code)
		}
	}
	cLocked, wLocked := testutil.NewTestContext(http.MethodGet, "/api/config/basic?key=prov-secret-1234")
	cLocked.Request.RemoteAddr = "198.51.100.77:12345"
	ManageHd.SiteBasicConfig(cLocked)
	respLocked := testutil.DecodeResponse(t, wLocked)
	rawLocked, _ := json.Marshal(respLocked.Data)
	if strings.Contains(string(rawLocked), "Secret Notice") {
		t.Fatalf("expected rate-limited IP on SiteBasicConfig to remain redacted, got %s", string(rawLocked))
	}
}

func TestCors_UntrustedPeerIgnoresForwardedHost(t *testing.T) {
	// CORS 公网直连对端伪造 X-Forwarded-Host 不得放行跨域凭证
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Cors())
	r.GET("/ping", func(c *gin.Context) { c.String(200, "ok") })

	req, _ := http.NewRequest(http.MethodGet, "/ping", nil)
	req.Host = "real.example.com"
	req.RemoteAddr = "198.51.100.10:54321"
	req.Header.Set("Origin", "https://evil.attacker.com")
	req.Header.Set("X-Forwarded-Host", "evil.attacker.com")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected untrusted peer X-Forwarded-Host ignored, got Allow-Origin=%q", got)
	}
}

func TestCors_RejectsCrossSchemeOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Cors())
	r.GET("/api/health", func(c *gin.Context) { c.Status(http.StatusOK) })

	// 可信代理传回 https，跨协议 http 同域 Origin 必须拒绝
	req1 := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req1.Host = "real.example.com"
	req1.RemoteAddr = "127.0.0.1:12345"
	req1.Header.Set("Origin", "http://real.example.com")
	req1.Header.Set("X-Forwarded-Proto", "https")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	if got := w1.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected cross-scheme origin rejected, got Allow-Origin=%q", got)
	}

	// 同协议 https 同域 Origin 应允许
	req2 := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req2.Host = "real.example.com"
	req2.RemoteAddr = "127.0.0.1:12345"
	req2.Header.Set("Origin", "https://real.example.com")
	req2.Header.Set("X-Forwarded-Proto", "https")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if got := w2.Header().Get("Access-Control-Allow-Origin"); got != "https://real.example.com" {
		t.Fatalf("expected same-scheme origin allowed, got Allow-Origin=%q", got)
	}
}
