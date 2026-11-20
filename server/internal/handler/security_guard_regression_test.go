package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/middleware"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	"server/internal/testutil"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
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
	if err := repository.SaveSiteBasic(cfg); err != nil {
		t.Fatal(err)
	}

	// 1. 匿名请求 SiteBasicConfig 不得暴露 ApiURL/AppID/AppSecret/ProvideKey
	c, w := testContext(http.MethodGet, "/api/config/basic")
	ManageHd.SiteBasicConfig(c)
	resp := decodeResponse(t, w)
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
	c2, w2 := testContext(http.MethodGet, "/api/manage/config/danmaku")
	c2.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 20001, Role: model.UserRoleVisitor})
	ManageHd.SiteDanmakuConfig(c2)
	resp2 := decodeResponse(t, w2)
	raw2, _ := json.Marshal(resp2.Data)
	if strings.Contains(string(raw2), "my-secret-token") {
		t.Fatalf("non-admin SiteDanmakuConfig leaked ApiURL: %s", string(raw2))
	}

	// 3. 超管可以看到完整配置
	c3, w3 := testContext(http.MethodGet, "/api/manage/config/danmaku")
	c3.Set(config.AuthUserClaims, &utils.UserClaims{UserID: config.UserIdInitialVal, Role: model.UserRoleAdmin})
	ManageHd.SiteDanmakuConfig(c3)
	resp3 := decodeResponse(t, w3)
	raw3, _ := json.Marshal(resp3.Data)
	if !strings.Contains(string(raw3), "my-secret-token") {
		t.Fatalf("admin SiteDanmakuConfig should retain ApiURL, got %s", string(raw3))
	}
}

func TestProvideHandler_RejectsExcessiveIDs(t *testing.T) {
	parts := make([]string, 105)
	for i := range parts {
		parts[i] = "1"
	}
	c, w := testContext(http.MethodGet, "/api/provide/vod?ac=detail&ids="+strings.Join(parts, ","))
	ProvideHd.HandleProvide(c)
	if !strings.Contains(w.Body.String(), "不能超过 100") {
		t.Fatalf("expected excessive ids rejection, got %s", w.Body.String())
	}
}

func TestDanmakuHandler_PostValidationAndCORSUntrustedPeer(t *testing.T) {
	// 1. Danmaku 参数防御（Mid<=0、非法颜色/mode）
	for _, payload := range []map[string]any{
		{"mid": 0, "time": 10, "text": "hello", "mode": 0},
		{"mid": 100, "time": -1, "text": "hello", "mode": 0},
		{"mid": 100, "time": 10, "text": "hello", "mode": 9},
		{"mid": 100, "time": 10, "text": "hello", "color": "javascript:1"},
	} {
		b, _ := json.Marshal(payload)
		c, w := testContext(http.MethodPost, "/api/film/danmaku")
		c.Request, _ = http.NewRequest(http.MethodPost, "/api/film/danmaku", bytes.NewReader(b))
		c.Request.Header.Set("Content-Type", "application/json")
		DanmakuHd.PostDanmaku(c)
		resp := decodeResponse(t, w)
		if resp.Code != dto.FAILED {
			t.Fatalf("expected validation failure for %+v, got %+v", payload, resp)
		}
	}

	// 2. CORS 公网直连对端伪造 X-Forwarded-Host 不得放行跨域凭证
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
