package collect

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"server/internal/model"
)

func TestCollectApiTestWithTimeout_XML(t *testing.T) {
	sampleXML := `<?xml version="1.0" encoding="utf-8"?><rss version="5.1"><list page="1" pagecount="1" pagesize="20" recordcount="0"></list></rss>`
	xmlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write([]byte(sampleXML))
	}))
	defer xmlSrv.Close()

	if err := CollectApiTest(model.FilmSource{Uri: xmlSrv.URL, Format: model.SourceFormatXML}); err != nil {
		t.Fatalf("expected xml test to pass, got err: %v", err)
	}

	err := CollectApiTest(model.FilmSource{Uri: xmlSrv.URL, Format: model.SourceFormatJSON})
	if err == nil || !strings.Contains(err.Error(), "接口返回为 XML 格式，与所选的 JSON 格式不一致") {
		t.Fatalf("expected format mismatch error, got: %v", err)
	}

	jsonSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"code":1,"msg":"数据列表","page":1,"pagecount":1,"limit":20,"total":0,"list":[]}`))
	}))
	defer jsonSrv.Close()

	err = CollectApiTest(model.FilmSource{Uri: jsonSrv.URL, Format: model.SourceFormatXML})
	if err == nil || !strings.Contains(err.Error(), "接口返回为 JSON 格式，与所选的 XML 格式不一致") {
		t.Fatalf("expected format mismatch error, got: %v", err)
	}
}

func TestCollectApiTest_HTMLResponseDetection(t *testing.T) {
	htmlResp := `<!DOCTYPE html><html><head><title>502 Bad Gateway</title></head><body><h1>502 Bad Gateway</h1></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(htmlResp))
	}))
	defer srv.Close()

	err := CollectApiTest(model.FilmSource{Name: "测试站", Uri: srv.URL, Format: model.SourceFormatJSON})
	if err == nil || !strings.Contains(err.Error(), "接口返回了 HTML 网页内容") {
		t.Fatalf("expected HTML warning message, got: %v", err)
	}
}
