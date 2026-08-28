package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
