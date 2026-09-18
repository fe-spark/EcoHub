package spider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
