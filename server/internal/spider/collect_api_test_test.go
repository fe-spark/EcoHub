package spider

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"server/internal/model"
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
	err := CollectApiTestChoosingProxy(model.FilmSource{Name: "t", Uri: "http://127.0.0.1:1"}, true)
	if err == nil || !strings.Contains(err.Error(), "代理未开启") {
		t.Fatalf("err=%v", err)
	}
}
