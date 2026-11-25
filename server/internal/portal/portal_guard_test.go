package portal

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"server/internal/model/dto"

	"github.com/gin-gonic/gin"
)

func TestProvideHandler_RejectsExcessiveIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parts := make([]string, 105)
	for i := range parts {
		parts[i] = "1"
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/provide/vod?ac=detail&ids="+strings.Join(parts, ","), nil)
	ProvideHd.HandleProvide(c)
	if !strings.Contains(w.Body.String(), "不能超过 100") {
		t.Fatalf("expected excessive ids rejection, got %s", w.Body.String())
	}
}

func TestDanmakuHandler_PostValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, payload := range []map[string]any{
		{"mid": 0, "time": 10, "text": "hello", "mode": 0},
		{"mid": 100, "time": -1, "text": "hello", "mode": 0},
		{"mid": 100, "time": 10, "text": "hello", "mode": 9},
		{"mid": 100, "time": 10, "text": "hello", "color": "javascript:1"},
	} {
		b, _ := json.Marshal(payload)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/film/danmaku", bytes.NewReader(b))
		c.Request.Header.Set("Content-Type", "application/json")
		DanmakuHd.PostDanmaku(c)
		var resp dto.Response
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v body=%s", err, w.Body.String())
		}
		if resp.Code != dto.FAILED {
			t.Fatalf("expected validation failure for %+v, got %+v", payload, resp)
		}
	}
}
