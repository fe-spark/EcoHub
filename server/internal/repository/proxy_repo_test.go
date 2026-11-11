package repository

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupProxyRepoTestDB(t *testing.T) *gorm.DB {
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
	return gdb
}

func TestDefaultProxyConfig_DanmakuOffOthersOn(t *testing.T) {
	m := DefaultProxyConfig().Modules
	if m.Danmaku {
		t.Fatal("default danmaku module should be off")
	}
	if !m.Spider || !m.TMDB || !m.Notify || !m.Upgrade {
		t.Fatalf("other modules should stay on by default: %+v", m)
	}
}

func TestResolveDanmakuProxy(t *testing.T) {
	const proxy = "http://127.0.0.1:7890"
	cases := []struct {
		name    string
		cfg     *model.ProxyConfig
		wantOK  bool
		wantURL string
	}{
		{name: "no record: global off", cfg: nil},
		{name: "global off", cfg: &model.ProxyConfig{Enabled: false, ProxyURL: proxy, Modules: model.ProxyModuleScope{Danmaku: true}}},
		{name: "module off", cfg: &model.ProxyConfig{Enabled: true, ProxyURL: proxy, Modules: model.ProxyModuleScope{Spider: true, Danmaku: false}}},
		{name: "module on", cfg: &model.ProxyConfig{Enabled: true, ProxyURL: proxy, Modules: model.ProxyModuleScope{Danmaku: true}}, wantOK: true, wantURL: proxy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupProxyRepoTestDB(t)
			if tc.cfg != nil {
				if err := SaveProxyConfig(*tc.cfg); err != nil {
					t.Fatal(err)
				}
			}
			ok, u, err := ResolveDanmakuProxy()
			if err != nil {
				t.Fatal(err)
			}
			if ok != tc.wantOK || u != tc.wantURL {
				t.Fatalf("got (%v,%q), want (%v,%q)", ok, u, tc.wantOK, tc.wantURL)
			}
		})
	}
}

// 已有安装保存的配置里没有 danmaku 字段：弹幕保持直连（模块关闭），其他模块不变。
func TestResolveDanmakuProxy_LegacyRecordWithoutDanmaku(t *testing.T) {
	gdb := setupProxyRepoTestDB(t)
	legacy := `{"enabled":true,"proxyUrl":"socks5://10.0.0.1:1080","modules":{"spider":true,"tmdb":false,"notify":true,"upgrade":false}}`
	if err := gdb.Create(&model.ProxyConfigRecord{Payload: legacy}).Error; err != nil {
		t.Fatal(err)
	}
	ok, u, err := ResolveDanmakuProxy()
	if err != nil || ok || u != "" {
		t.Fatalf("danmaku got (%v,%q,%v), want direct", ok, u, err)
	}
	if ok, u, _ := ResolveSpiderProxy(); !ok || u != "socks5://10.0.0.1:1080" {
		t.Fatalf("spider should still use proxy, got (%v,%q)", ok, u)
	}
	if ok, _, _ := ResolveTMDBProxy(); ok {
		t.Fatal("tmdb module should stay off")
	}
}
