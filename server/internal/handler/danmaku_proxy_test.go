package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupDanmakuProxyDB(t *testing.T, cfg *model.ProxyConfig) {
	t.Helper()
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
	if cfg != nil {
		if err := repository.SaveProxyConfig(*cfg); err != nil {
			t.Fatal(err)
		}
	}
}

// 代理开启且弹幕模块开启：弹幕请求经过后台配置的代理。
func TestDanmakuFetch_UsesAdminProxyWhenModuleOn(t *testing.T) {
	var (
		mu   sync.Mutex
		seen []string
	)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.String())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"animes":  []map[string]any{{"episodes": []map[string]any{{"episodeId": 4242, "episodeTitle": "第1集"}}}},
		})
	}))
	defer proxy.Close()

	setupDanmakuProxyDB(t, &model.ProxyConfig{Enabled: true, ProxyURL: proxy.URL, Modules: model.ProxyModuleScope{Danmaku: true}})

	got := searchDandanplayEpisode(model.DanmakuConfig{}, "http://danmaku.example.invalid/api/v2", "测试剧", 1)
	if got != 4242 {
		t.Fatalf("episode id = %d, want 4242", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0] == "" || seen[0][:len("http://danmaku.example.invalid/")] != "http://danmaku.example.invalid/" {
		t.Fatalf("proxy saw %v, want absolute URL to danmaku.example.invalid", seen)
	}
}

// 代理关闭、或弹幕模块关闭：直连，且不读取 HTTPS_PROXY 等环境变量。
func TestDanmakuHTTPClient_DirectWhenOffIgnoresEnv(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	cases := map[string]*model.ProxyConfig{
		"no record":  nil,
		"global off": {Enabled: false, ProxyURL: "http://127.0.0.1:7890", Modules: model.ProxyModuleScope{Danmaku: true}},
		"module off": {Enabled: true, ProxyURL: "http://127.0.0.1:7890", Modules: model.ProxyModuleScope{Spider: true}},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			setupDanmakuProxyDB(t, cfg)
			c, err := danmakuHTTPClient(0)
			if err != nil {
				t.Fatal(err)
			}
			tr, ok := c.Transport.(*http.Transport)
			if !ok || tr == nil {
				t.Fatalf("transport = %T, want *http.Transport", c.Transport)
			}
			if tr.Proxy != nil {
				t.Fatal("direct client must not have a Proxy func (would read env proxies)")
			}
		})
	}
}
