package service

import (
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
		Enabled:  true,
		ProxyURL: "  http://127.0.0.1:7890/  ",
	}

	norm := repository.NormalizeProxyConfig(cfg)
	if norm.ProxyURL != "http://127.0.0.1:7890/" {
		t.Errorf("expected trimmed proxyUrl, got %s", norm.ProxyURL)
	}
}

func TestResolveSourceProxy(t *testing.T) {
	// 默认未配置或未开启
	if ok, _ := repository.ResolveSourceProxy("s1"); ok {
		t.Errorf("expected false when proxy is not configured or disabled")
	}
}

func TestTestProxyEmptyURL(t *testing.T) {
	_, err := ProxySvc.TestProxy("")
	if err == nil {
		t.Errorf("expected error for empty proxy url")
	}
}

func TestResolveModuleProxies(t *testing.T) {
	if ok, _ := repository.ResolveTMDBProxy(); ok {
		t.Errorf("expected false when disabled")
	}
	if ok, _ := repository.ResolveNotifyProxy(); ok {
		t.Errorf("expected false when disabled")
	}
	if ok, _ := repository.ResolveUpgradeProxy(); ok {
		t.Errorf("expected false when disabled")
	}
}
