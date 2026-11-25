package admin

import (
	"server/internal/repobase"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
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

	norm := repobase.NormalizeProxyConfig(cfg)
	if norm.ProxyURL != "http://127.0.0.1:7890/" {
		t.Errorf("expected trimmed proxyUrl, got %s", norm.ProxyURL)
	}
}

func TestUpdateConfig_ValidatesDisabledURLAndPublicHasAuth(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.ProxyConfigRecord{}); err != nil {
		t.Fatal(err)
	}
	prevM, prevR := db.Mdb, db.Rdb
	db.Mdb, db.Rdb = gdb, nil
	t.Cleanup(func() { db.Mdb, db.Rdb = prevM, prevR })

	// 关闭状态下填写非法协议也应被拦截
	err = ProxySvc.UpdateConfig(model.ProxyConfig{
		Enabled:  false,
		ProxyURL: "ftp://127.0.0.1:21",
		Modules:  model.ProxyModuleScope{Spider: true},
	}, false)
	if err == nil {
		t.Fatal("expected error for invalid proxyUrl even when disabled")
	}

	// 保存带认证凭据的代理配置，PublicConfig 应返回 HasAuth=true 且 URL 已脱敏
	if err := ProxySvc.UpdateConfig(model.ProxyConfig{
		Enabled:  true,
		ProxyURL: "http://alice:secret@127.0.0.1:7890",
		Modules:  model.ProxyModuleScope{Spider: true},
	}, false); err != nil {
		t.Fatalf("unexpected UpdateConfig error: %v", err)
	}
	pub, err := ProxySvc.PublicConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !pub.HasAuth || pub.ProxyURL != "http://127.0.0.1:7890" {
		t.Fatalf("unexpected PublicConfig: %+v", pub)
	}

	// 显式清除认证（preserveAuth=false）后 HasAuth 应变为 false
	if err := ProxySvc.UpdateConfig(model.ProxyConfig{
		Enabled:  true,
		ProxyURL: "http://127.0.0.1:7890",
		Modules:  model.ProxyModuleScope{Spider: true},
	}, false); err != nil {
		t.Fatalf("unexpected clear auth UpdateConfig error: %v", err)
	}
	pubCleared, err := ProxySvc.PublicConfig()
	if err != nil {
		t.Fatal(err)
	}
	if pubCleared.HasAuth {
		t.Fatalf("expected HasAuth=false after clearing auth, got %+v", pubCleared)
	}
}

func TestTestProxyEmptyURL(t *testing.T) {
	_, err := ProxySvc.TestProxy("", false)
	if err == nil {
		t.Errorf("expected error for empty proxy url")
	}
}

func TestNormalizeAndValidateProxyURL(t *testing.T) {
	got, err := normalizeAndValidateProxyURL("SOCKS5H://127.0.0.1:1080")
	if err != nil {
		t.Fatalf("unexpected error for socks5h: %v", err)
	}
	if got != "socks5h://127.0.0.1:1080" {
		t.Fatalf("got %q, want socks5h://127.0.0.1:1080", got)
	}

	gotDefault, err := normalizeAndValidateProxyURL("127.0.0.1:7890")
	if err != nil {
		t.Fatalf("unexpected error for host:port: %v", err)
	}
	if gotDefault != "http://127.0.0.1:7890" {
		t.Fatalf("got %q, want http://127.0.0.1:7890", gotDefault)
	}

	if _, err := normalizeAndValidateProxyURL("ftp://127.0.0.1:21"); err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
}

func TestResolveEffectiveProxyURL_PreserveAuth(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.ProxyConfigRecord{}); err != nil {
		t.Fatal(err)
	}
	prevM, prevR := db.Mdb, db.Rdb
	db.Mdb, db.Rdb = gdb, nil
	t.Cleanup(func() { db.Mdb, db.Rdb = prevM, prevR })

	saved := model.ProxyConfig{
		Enabled:  true,
		ProxyURL: "socks5h://alice:secret@127.0.0.1:1080",
		Modules:  model.ProxyModuleScope{Spider: true, Notify: true},
	}
	if err := repobase.SaveProxyConfig(saved); err != nil {
		t.Fatal(err)
	}

	// 测试连接或保存时未修改脱敏 URL，应保留账密
	got, err := resolveEffectiveProxyURL("socks5h://127.0.0.1:1080", true)
	if err != nil {
		t.Fatal(err)
	}
	if got != saved.ProxyURL {
		t.Fatalf("got %q, want %q", got, saved.ProxyURL)
	}

	// 主动修改为无账密地址（preserveAuth=false），不应回填旧账密
	gotNoAuth, err := resolveEffectiveProxyURL("socks5h://127.0.0.1:1080", false)
	if err != nil {
		t.Fatal(err)
	}
	if gotNoAuth != "socks5h://127.0.0.1:1080" {
		t.Fatalf("got %q, want socks5h://127.0.0.1:1080", gotNoAuth)
	}
}

func TestResolveModuleProxies(t *testing.T) {
	if ok, _, err := repobase.ResolveTMDBProxy(); err == nil && ok {
		t.Fatal("未配置代理时不能启用")
	}
	if ok, _, err := repobase.ResolveUpgradeProxy(); err == nil && ok {
		t.Fatal("未配置代理时不能启用")
	}
}
