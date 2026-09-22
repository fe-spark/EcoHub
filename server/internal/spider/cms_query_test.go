package spider

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"server/internal/model"
)

func TestSearchSourceListAndFetchDetails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ac := r.URL.Query().Get("ac")
		w.Header().Set("Content-Type", "application/json")
		if ac == "list" {
			_ = json.NewEncoder(w).Encode(model.FilmListPage{
				Code:      1,
				Page:      1,
				PageCount: 1,
				Limit:     20,
				Total:     1,
				List: []model.FilmList{{
					VodID:      88,
					VodName:    "仙逆",
					VodPic:     "http://pic/x.jpg",
					VodRemarks: "更新至10集",
					TypeName:   "动漫",
				}},
			})
			return
		}
		if ac == "detail" && r.URL.Query().Get("ids") == "88" {
			_ = json.NewEncoder(w).Encode(model.FilmDetailLPage{
				List: []model.FilmDetail{{
					VodID:       88,
					VodName:     "仙逆",
					VodPic:      "http://pic/x.jpg",
					VodPlayFrom: "m3u8",
					VodPlayURL:  "第1集$http://play/1.m3u8",
				}},
			})
			return
		}
		http.Error(w, "bad request", http.StatusBadRequest)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	page, err := SearchSourceList(srv.URL, "仙逆", 1)
	if err != nil {
		t.Fatalf("SearchSourceList: %v", err)
	}
	if len(page.List) != 1 || page.List[0].VodID != 88 {
		t.Fatalf("unexpected list: %+v", page.List)
	}

	details, err := FetchSourceDetails(srv.URL, "88")
	if err != nil {
		t.Fatalf("FetchSourceDetails: %v", err)
	}
	if len(details) != 1 || details[0].Name != "仙逆" || len(details[0].PlayList) == 0 {
		t.Fatalf("unexpected details: %+v", details)
	}
}

func TestSearchSourceListEmptyKeyword(t *testing.T) {
	if _, err := SearchSourceList("http://example", "  ", 1); err == nil {
		t.Fatal("empty keyword should fail")
	}
}

func TestIsCMSSearchUnsupportedBody(t *testing.T) {
	cases := []struct {
		body string
		want bool
	}{
		{"暂不支持搜索", true},
		{"  暂不支持搜索  ", true},
		{"err not serarch", true},
		{"ERR NOT SEARCH", true},
		{`{"code":1,"list":[]}`, false},
		{"", false},
		{"源站维护中", false},
		{`<html><body>` + string(make([]byte, 200)) + `</body></html>`, false},
	}
	for _, tc := range cases {
		if got := isCMSSearchUnsupportedBody(tc.body); got != tc.want {
			t.Fatalf("body=%q got=%v want=%v", tc.body, got, tc.want)
		}
	}
}

func TestSearchSourceListUnsupported(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html;charset=utf-8")
		_, _ = w.Write([]byte("暂不支持搜索"))
	}))
	t.Cleanup(srv.Close)

	_, err := SearchSourceList(srv.URL, "2", 1)
	if !errors.Is(err, ErrCMSSearchUnsupported) {
		t.Fatalf("first call err=%v", err)
	}
	_, err = SearchSourceList(srv.URL, "2", 1)
	if !errors.Is(err, ErrCMSSearchUnsupported) {
		t.Fatalf("cached call err=%v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected 1 upstream hit, got %d", hits.Load())
	}
}

func TestSearchSourceListErrNotSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html;charset=utf-8")
		_, _ = w.Write([]byte("err not serarch"))
	}))
	t.Cleanup(srv.Close)

	_, err := SearchSourceList(srv.URL, "处女", 1)
	if !errors.Is(err, ErrCMSSearchUnsupported) {
		t.Fatalf("err=%v", err)
	}
	if err.Error() != ErrCMSSearchUnsupported.Error() {
		t.Fatalf("user-facing detail leaked: %v", err)
	}
}

func TestSearchSourceListDoesNotCacheGenericShortBody(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("源站维护中"))
	}))
	t.Cleanup(srv.Close)

	_, err := SearchSourceList(srv.URL, "2", 1)
	if errors.Is(err, ErrCMSSearchUnsupported) {
		t.Fatal("generic short body must not be treated as unsupported")
	}
	_, err = SearchSourceList(srv.URL, "2", 1)
	if errors.Is(err, ErrCMSSearchUnsupported) {
		t.Fatal("generic short body must not be cached as unsupported")
	}
	if hits.Load() != 2 {
		t.Fatalf("expected 2 upstream hits, got %d", hits.Load())
	}
}
