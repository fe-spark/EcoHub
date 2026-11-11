package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

	got, err := searchDandanplayEpisode(model.DanmakuConfig{}, "http://danmaku.example.invalid/api/v2", "测试剧", 1)
	if err != nil || got.EpisodeID != 4242 {
		t.Fatalf("episode = %+v err=%v, want 4242", got, err)
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

// 后台「测试」走 fetchSniffedDanmaku(useCache=false)，同样经过后台代理。
func TestDanmakuAdminTestPath_UsesAdminProxy(t *testing.T) {
	var (
		mu   sync.Mutex
		seen []string
	)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/comment/") {
			_, _ = w.Write([]byte(`{"count":1,"comments":[{"p":"1,1,16777215,u","m":"经过代理"}]}`))
			return
		}
		_, _ = w.Write([]byte(fakeSearchHit))
	}))
	defer proxy.Close()

	setupDanmakuProxyDB(t, &model.ProxyConfig{Enabled: true, ProxyURL: proxy.URL, Modules: model.ProxyModuleScope{Danmaku: true}})
	cfg := model.DanmakuConfig{Enabled: true, AutoSniff: true, ApiURL: "http://danmaku.example.invalid/api/v2"}
	res := fetchSniffedDanmaku(cfg, "测试动画", 1, false)
	if res.Status != networkOK || len(res.Items) != 1 {
		t.Fatalf("status=%s count=%d reason=%q", res.Status, len(res.Items), res.Reason)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("proxy saw %v, want search + comment", seen)
	}
}

// 读不到代理配置时不发请求，返回可读的失败原因。
func TestFetchSniffedDanmaku_ProxyConfigUnreadable(t *testing.T) {
	prevM, prevR := db.Mdb, db.Rdb
	db.Mdb, db.Rdb = nil, nil
	t.Cleanup(func() { db.Mdb, db.Rdb = prevM, prevR })

	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer srv.Close()
	res := fetchSniffedDanmaku(model.DanmakuConfig{Enabled: true, AutoSniff: true, ApiURL: srv.URL + "/api/v2"}, "测试动画", 1, false)
	if res.Status != networkError || res.Reason != "读取网络代理配置失败" || hit {
		t.Fatalf("status=%s reason=%q hit=%v", res.Status, res.Reason, hit)
	}
}
