package spider

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCollectApiTestChoosingProxyDirect(t *testing.T) {
	var sawProxy bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Connection") != "" {
			sawProxy = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"msg":"ok","page":1,"pagecount":1,"limit":20,"total":0,"list":[]}`))
	}))
	t.Cleanup(srv.Close)

	err := CollectApiTestChoosingProxy(model.FilmSource{Name: "t", Uri: srv.URL}, false)
	if err != nil {
		t.Fatal(err)
	}
	if sawProxy {
		t.Fatal("direct test should not use a proxy")
	}
}

func TestCollectApiTestChoosingProxyRequiresConfig(t *testing.T) {
	// 本用例要的是"数据库未连接"，显式置空，避免读到其他用例留下的 db.Mdb。
	prev := db.Mdb
	db.Mdb = nil
	t.Cleanup(func() { db.Mdb = prev })

	err := CollectApiTestChoosingProxy(model.FilmSource{Name: "t", Uri: "http://127.0.0.1:1"}, true)
	if err == nil || !strings.Contains(err.Error(), "数据库未连接") {
		t.Fatalf("err=%v", err)
	}
}

func TestCollectApiTest_ProxyCollectRequiresGlobalProxy(t *testing.T) {
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

	if err := repository.SaveProxyConfig(model.ProxyConfig{
		Enabled: false,
		Modules: model.ProxyModuleScope{Spider: true},
	}); err != nil {
		t.Fatal(err)
	}

	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
	}))
	t.Cleanup(srv.Close)

	err = CollectApiTest(model.FilmSource{Name: "p", Uri: srv.URL, ProxyCollect: true})
	if err == nil || !strings.Contains(err.Error(), "未开启") {
		t.Fatalf("expected global proxy disabled error, got err=%v", err)
	}
	if hit {
		t.Fatal("must not fall back to direct request when ProxyCollect is true and global proxy is off")
	}
}
