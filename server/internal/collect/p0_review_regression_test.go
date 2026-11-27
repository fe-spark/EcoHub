package collect

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"server/internal/utils"
)

func TestGetPageCount_RejectsAbnormalPageCount(t *testing.T) {
	jsonSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"pagecount":2147483647,"total":999999999}`))
	}))
	defer jsonSrv.Close()

	if _, err := (&JsonCollect{}).GetPageCount(utils.RequestInfo{Uri: jsonSrv.URL, Params: url.Values{}}); err == nil {
		t.Fatal("JsonCollect.GetPageCount 应拒绝超大 pagecount")
	}

	xmlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?><rss version="5.1"><list page="1" pagecount="9999999" pagesize="20" recordcount="100"></list></rss>`))
	}))
	defer xmlSrv.Close()

	if _, err := (&XmlCollect{}).GetPageCount(utils.RequestInfo{Uri: xmlSrv.URL}); err == nil {
		t.Fatal("XmlCollect.GetPageCount 应拒绝超大 pagecount")
	}
}
