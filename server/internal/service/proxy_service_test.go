package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"server/internal/model"
	"server/internal/repository"
)

func TestRedactProxyURL(t *testing.T) {
	got := RedactProxyURL("http://user:secret@127.0.0.1:7890")
	if got != "http://127.0.0.1:7890" {
		t.Fatalf("redact = %s", got)
	}
	if proxyURLHasUser("socks5://127.0.0.1:1080") {
		t.Fatal("expected no user")
	}
	if !proxyURLHasUser("socks5://u:p@127.0.0.1:1080") {
		t.Fatal("expected user")
	}
}

func TestNormalizeProxyConfig(t *testing.T) {
	cfg := model.ProxyConfig{
		Enabled:   true,
		ProxyURL:  "  http://127.0.0.1:7890/  ",
		Scope:     "invalid_scope",
		SourceIds: []string{"s1", "s2", "s1", "  ", "s3"},
	}

	norm := repository.NormalizeProxyConfig(cfg)
	if norm.ProxyURL != "http://127.0.0.1:7890/" {
		t.Errorf("expected trimmed proxyUrl, got %s", norm.ProxyURL)
	}
	if norm.Scope != model.ProxyScopeAll {
		t.Errorf("expected default scope 'all', got %s", norm.Scope)
	}
	if len(norm.SourceIds) != 3 {
		t.Errorf("expected 3 unique sourceIds, got %d", len(norm.SourceIds))
	}
}

func TestResolveSourceProxy(t *testing.T) {
	// 默认未配置或未开启
	if ok, _ := repository.ResolveSourceProxy("s1"); ok {
		t.Errorf("expected false when proxy is not configured or disabled")
	}
}

func TestTestProxyWithServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	// 使用直接地址测试 TestProxy 错误处理
	_, err := ProxySvc.TestProxy("", ts.URL)
	if err == nil {
		t.Errorf("expected error for empty proxy url")
	}
}
