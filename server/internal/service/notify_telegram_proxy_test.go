package service

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
)

func telegramTransport(t *testing.T) *http.Transport {
	t.Helper()
	c, err := getTelegramHTTPClient(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr == nil {
		t.Fatalf("transport = %T, want *http.Transport", c.Transport)
	}
	return tr
}

// 后台代理没开、或 Telegram 模块关闭时直连，不再读 HTTPS_PROXY 等环境变量。
func TestTelegramClient_DirectIgnoresEnvProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	cases := map[string]*model.ProxyConfig{
		"no record":  nil,
		"global off": {Enabled: false, ProxyURL: "http://127.0.0.1:7890", Modules: model.ProxyModuleScope{Notify: true}},
		"module off": {Enabled: true, ProxyURL: "http://127.0.0.1:7890", Modules: model.ProxyModuleScope{Spider: true}},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			setupNotifyConfigDB(t)
			prev := db.Rdb
			db.Rdb = nil
			t.Cleanup(func() { db.Rdb = prev })
			if cfg != nil {
				if err := repository.SaveProxyConfig(*cfg); err != nil {
					t.Fatal(err)
				}
			}
			if tr := telegramTransport(t); tr.Proxy != nil {
				t.Fatal("direct Telegram client must not have a Proxy func")
			}
		})
	}
}

func TestTelegramClient_UsesAdminProxyWhenModuleOn(t *testing.T) {
	setupNotifyConfigDB(t)
	prev := db.Rdb
	db.Rdb = nil
	t.Cleanup(func() { db.Rdb = prev })
	if err := repository.SaveProxyConfig(model.ProxyConfig{Enabled: true, ProxyURL: "http://127.0.0.1:7890", Modules: model.ProxyModuleScope{Notify: true}}); err != nil {
		t.Fatal(err)
	}
	tr := telegramTransport(t)
	if tr.Proxy == nil {
		t.Fatal("expected proxy func")
	}
	req, _ := http.NewRequest(http.MethodGet, "https://api.telegram.org/bot1/getMe", nil)
	u, err := tr.Proxy(req)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := url.Parse("http://127.0.0.1:7890"); u == nil || u.String() != want.String() {
		t.Fatalf("proxy = %v", u)
	}
}
