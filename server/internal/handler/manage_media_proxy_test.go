package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"server/internal/model"

	"github.com/gin-gonic/gin"
)

func TestSiteMediaProxyConfig_GetAndPost(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 测试默认配置获取
	t.Run("default media proxy config", func(t *testing.T) {
		def := model.DefaultMediaProxyConfig()
		if !def.Enabled {
			t.Errorf("expected default media proxy enabled to be true, got %v", def.Enabled)
		}
		if def.Template != "" {
			t.Errorf("expected default template to be empty, got %q", def.Template)
		}
	})

	t.Run("service update and retrieval", func(t *testing.T) {
		// Mock test without full DB: verify JSON encode/decode
		cfg := model.MediaProxyConfig{
			Enabled:  true,
			Template: "https://my-worker.workers.dev/{url}",
		}
		encoded, err := model.EncodeMediaProxyJSON(cfg)
		if err != nil {
			t.Fatalf("EncodeMediaProxyJSON error: %v", err)
		}
		decoded, err := model.DecodeMediaProxyJSON(encoded)
		if err != nil {
			t.Fatalf("DecodeMediaProxyJSON error: %v", err)
		}
		if decoded.Enabled != cfg.Enabled || decoded.Template != cfg.Template {
			t.Errorf("expected %+v, got %+v", cfg, decoded)
		}

		// Empty raw string should return default
		emptyDecoded, err := model.DecodeMediaProxyJSON("")
		if err != nil {
			t.Fatalf("DecodeMediaProxyJSON empty string error: %v", err)
		}
		if !emptyDecoded.Enabled {
			t.Errorf("expected enabled=true on empty raw, got %v", emptyDecoded.Enabled)
		}
	})

	t.Run("handler bind json validation", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		body := []byte(`{"enabled":false,"template":" https://custom.proxy.com/{url} "}`)
		c.Request, _ = http.NewRequest(http.MethodPost, "/api/manage/config/media-proxy", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		var payload model.MediaProxyConfig
		if err := c.ShouldBindJSON(&payload); err != nil {
			t.Fatalf("ShouldBindJSON failed: %v", err)
		}
		if payload.Enabled {
			t.Errorf("expected Enabled=false, got true")
		}
		if payload.Template != " https://custom.proxy.com/{url} " {
			t.Errorf("unexpected template: %q", payload.Template)
		}
	})

	t.Run("handler template validation", func(t *testing.T) {
		// Missing {url} should fail
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		body := []byte(`{"enabled":true,"template":"https://custom.proxy.com/"}`)
		c.Request, _ = http.NewRequest(http.MethodPost, "/api/manage/config/media-proxy", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		ManageHd.UpdateSiteMediaProxy(c)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 with error dto, got %d", w.Code)
		}
		if !bytes.Contains(w.Body.Bytes(), []byte("{url}")) {
			t.Errorf("expected error message mentioning {url}, got: %s", w.Body.String())
		}

		// Invalid scheme should fail
		w2 := httptest.NewRecorder()
		c2, _ := gin.CreateTestContext(w2)
		body2 := []byte(`{"enabled":true,"template":"ftp://custom.proxy.com/{url}"}`)
		c2.Request, _ = http.NewRequest(http.MethodPost, "/api/manage/config/media-proxy", bytes.NewReader(body2))
		c2.Request.Header.Set("Content-Type", "application/json")
		ManageHd.UpdateSiteMediaProxy(c2)
		if !bytes.Contains(w2.Body.Bytes(), []byte("http")) {
			t.Errorf("expected error message mentioning http, got: %s", w2.Body.String())
		}
	})
}
