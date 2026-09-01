package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"server/internal/config"

	"github.com/gin-gonic/gin"
)

func TestAccessLogMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	origEnabled := config.AccessLogEnabled
	origSlowMs := config.AccessSlowMs
	t.Cleanup(func() {
		config.AccessLogEnabled = origEnabled
		config.AccessSlowMs = origSlowMs
	})
	config.AccessLogEnabled = true
	config.AccessSlowMs = 500

	r := gin.New()
	r.Use(AccessLog())
	r.GET("/api/test-ok", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})
	r.GET("/api/test-err", func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"message": "not found"})
	})

	t.Run("normal 200 request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/test-ok?foo=bar", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})

	t.Run("error 404 request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/test-err?foo=baz", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})
}

func TestSanitizeAccessLogURI(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"/api/index", "/api/index"},
		{"/api/test\n\r?foo=bar", "/api/test?foo=bar"},
		{strings.Repeat("a", 600), strings.Repeat("a", 512) + "..."},
		{strings.Repeat("中", 600), strings.Repeat("中", 512) + "..."},
	}

	for _, tc := range cases {
		got := sanitizeAccessLogURI(tc.input)
		if got != tc.expected {
			t.Errorf("sanitizeAccessLogURI(%q) = %q; want %q", tc.input, got, tc.expected)
		}
	}
}
