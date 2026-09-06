package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"server/internal/config"

	"github.com/gin-gonic/gin"
)

func TestTrackViewMaxBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	raw := `{"action":"browse","resource":"` + strings.Repeat("a", 8000) + `"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/stat/view", strings.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	AccessHd.TrackView(c)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("want ok envelope, got %+v", resp)
	}
}

func TestTrackViewValidPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	raw := `{"action":"browse","path":"/filmClassify","page_title":"分类","device_id":"eh_did_12345"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/stat/view", strings.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	AccessHd.TrackView(c)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("want code 0, got %+v", resp)
	}
}

func TestAccessOverviewAndTopsHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	orig := config.AccessLogEnabled
	defer func() { config.AccessLogEnabled = orig }()

	// 1. 关闭状态下测试
	config.AccessLogEnabled = false
	{
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manage/access/status", nil)
		AccessHd.Status(c)
		resp := decodeResponse(t, w)
		if resp.Code != 0 {
			t.Fatalf("want code 0, got %+v", resp)
		}

		w2 := httptest.NewRecorder()
		c2, _ := gin.CreateTestContext(w2)
		c2.Request = httptest.NewRequest(http.MethodGet, "/manage/access/overview?day=2020-01-01&module=web", nil)
		AccessHd.Overview(c2)
		resp2 := decodeResponse(t, w2)
		if resp2.Code == 0 {
			t.Fatalf("overview should fail when disabled, got %+v", resp2)
		}

		w3 := httptest.NewRecorder()
		c3, _ := gin.CreateTestContext(w3)
		c3.Request = httptest.NewRequest(http.MethodGet, "/manage/access/tops?day=2020-01-01&kind=play&module=web&limit=5", nil)
		AccessHd.Tops(c3)
		resp3 := decodeResponse(t, w3)
		if resp3.Code == 0 {
			t.Fatalf("tops should fail when disabled, got %+v", resp3)
		}

		w4 := httptest.NewRecorder()
		c4, _ := gin.CreateTestContext(w4)
		c4.Request = httptest.NewRequest(http.MethodGet, "/manage/access/logs?day=2020-01-01&module=web&limit=10", nil)
		AccessHd.Logs(c4)
		resp4 := decodeResponse(t, w4)
		if resp4.Code == 0 {
			t.Fatalf("logs should fail when disabled, got %+v", resp4)
		}
	}

	// 2. 开启状态下测试
	config.AccessLogEnabled = true
	{
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manage/access/status", nil)
		AccessHd.Status(c)
		resp := decodeResponse(t, w)
		if resp.Code != 0 {
			t.Fatalf("want code 0, got %+v", resp)
		}

		// 测试历史日期（无 Redis 时返回安全默认数据）
		w2 := httptest.NewRecorder()
		c2, _ := gin.CreateTestContext(w2)
		c2.Request = httptest.NewRequest(http.MethodGet, "/manage/access/overview?day=2020-01-01&module=web", nil)
		AccessHd.Overview(c2)
		if w2.Code != http.StatusOK {
			t.Fatalf("overview code=%d body=%s", w2.Code, w2.Body.String())
		}
		resp2 := decodeResponse(t, w2)
		if resp2.Code != 0 {
			t.Fatalf("want code 0, got %+v", resp2)
		}

		// 测试 Tops 榜单接口
		w3 := httptest.NewRecorder()
		c3, _ := gin.CreateTestContext(w3)
		c3.Request = httptest.NewRequest(http.MethodGet, "/manage/access/tops?day=2020-01-01&kind=play&module=web&limit=5", nil)
		AccessHd.Tops(c3)
		if w3.Code != http.StatusOK {
			t.Fatalf("tops code=%d body=%s", w3.Code, w3.Body.String())
		}
		resp3 := decodeResponse(t, w3)
		if resp3.Code != 0 {
			t.Fatalf("want code 0, got %+v", resp3)
		}

		// 测试 Logs 流水接口（当 Redis 不可用时安全返回错误响应而非 panic）
		w4 := httptest.NewRecorder()
		c4, _ := gin.CreateTestContext(w4)
		c4.Request = httptest.NewRequest(http.MethodGet, "/manage/access/logs?day=2020-01-01&module=web&limit=10", nil)
		AccessHd.Logs(c4)
		if w4.Code != http.StatusOK {
			t.Fatalf("logs code=%d body=%s", w4.Code, w4.Body.String())
		}
	}
}
