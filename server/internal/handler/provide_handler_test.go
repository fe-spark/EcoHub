package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"server/internal/infra/db"

	"github.com/gin-gonic/gin"
)

func TestNormalizeMediaURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		baseURL string
		want    string
	}{
		{
			name:    "empty raw",
			raw:     "",
			baseURL: "https://example.com",
			want:    "",
		},
		{
			name:    "empty baseURL",
			raw:     "/path/to/pic.jpg",
			baseURL: "",
			want:    "/path/to/pic.jpg",
		},
		{
			name:    "absolute http url",
			raw:     "http://img.test.com/pic.jpg",
			baseURL: "https://example.com",
			want:    "http://img.test.com/pic.jpg",
		},
		{
			name:    "absolute https url",
			raw:     "https://img.test.com/pic.jpg",
			baseURL: "https://example.com",
			want:    "https://img.test.com/pic.jpg",
		},
		{
			name:    "protocol-relative url with https base",
			raw:     "//img.doubanio.com/view/photo/p123.jpg",
			baseURL: "https://example.com",
			want:    "https://img.doubanio.com/view/photo/p123.jpg",
		},
		{
			name:    "protocol-relative url with http base",
			raw:     "//img.doubanio.com/view/photo/p123.jpg",
			baseURL: "http://example.com",
			want:    "http://img.doubanio.com/view/photo/p123.jpg",
		},
		{
			name:    "root-relative url",
			raw:     "/api/upload/pic/poster/abc.jpg",
			baseURL: "https://example.com",
			want:    "https://example.com/api/upload/pic/poster/abc.jpg",
		},
		{
			name:    "relative url without leading slash",
			raw:     "api/upload/pic/poster/abc.jpg",
			baseURL: "https://example.com",
			want:    "https://example.com/api/upload/pic/poster/abc.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeMediaURL(tt.raw, tt.baseURL)
			if got != tt.want {
				t.Errorf("normalizeMediaURL(%q, %q) = %q, want %q", tt.raw, tt.baseURL, got, tt.want)
			}
		})
	}
}

func TestHandleProvideConfig_RedisNilSafety(t *testing.T) {
	origRdb := db.Rdb
	db.Rdb = nil
	defer func() {
		db.Rdb = origRdb
	}()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/api/provide/config", nil)
	c.Request.Host = "127.0.0.1:8080"

	ProvideHd.HandleProvideConfig(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var res map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, ok := res["sites"]; !ok {
		t.Fatalf("expected sites in response")
	}
}
