package utils

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
)

func TestNewTmdbClient_ApiKeyResolution(t *testing.T) {
	// 1. 无 Key 时报错
	_ = os.Unsetenv("TMDB_API_KEY")
	_, err := NewTmdbClient("", "")
	if err != ErrNoTmdbApiKey {
		t.Fatalf("expected ErrNoTmdbApiKey, got %v", err)
	}

	// 2. 环境变量降级读取
	_ = os.Setenv("TMDB_API_KEY", "env-key-123")
	defer os.Unsetenv("TMDB_API_KEY")

	c, err := NewTmdbClient("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.apiKey != "env-key-123" {
		t.Errorf("expected env-key-123, got %s", c.apiKey)
	}
	if c.baseURL != defaultTmdbBaseURL {
		t.Errorf("expected default base URL %s, got %s", defaultTmdbBaseURL, c.baseURL)
	}

	// 3. 显式参数优先于环境变量
	c2, err := NewTmdbClient("custom-key-456", "https://api.tmdb.proxy.org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c2.apiKey != "custom-key-456" {
		t.Errorf("expected custom-key-456, got %s", c2.apiKey)
	}
	if c2.baseURL != "https://api.tmdb.proxy.org/3" {
		t.Errorf("expected normalized proxy URL https://api.tmdb.proxy.org/3, got %s", c2.baseURL)
	}
}

func TestTmdbClient_SearchAndGetDetail_Success(t *testing.T) {
	mux := http.NewServeMux()

	// 模拟 /search/movie
	mux.HandleFunc("/3/search/movie", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		if query != "沙丘" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{
			"page": 1,
			"results": [
				{
					"id": 438631,
					"title": "沙丘",
					"original_title": "Dune",
					"release_date": "2021-09-15",
					"vote_average": 7.8
				}
			]
		}`)
	})

	// 模拟 /movie/438631
	mux.HandleFunc("/3/movie/438631", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{
			"id": 438631,
			"title": "沙丘",
			"original_title": "Dune",
			"overview": "天赋异禀的少年保罗·厄崔迪被命运指引...",
			"release_date": "2021-09-15",
			"poster_path": "/d5NXSklXo0qyIYkgV94XAgMIckC.jpg",
			"backdrop_path": "/jYEW5xZkZk2WTrdbMGAPFuBqbDc.jpg",
			"vote_average": 7.842,
			"genres": [
				{"id": 878, "name": "科幻"},
				{"id": 12, "name": "冒险"}
			],
			"credits": {
				"cast": [
					{"name": "提莫西·查拉梅", "original_name": "Timothée Chalamet"},
					{"name": "丽贝卡·弗格森", "original_name": "Rebecca Ferguson"}
				],
				"crew": [
					{"name": "丹尼斯·维伦纽瓦", "original_name": "Denis Villeneuve", "job": "Director"}
				]
			}
		}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client, err := NewTmdbClientWithHTTPClient("dummy-key", server.URL, server.Client())
	if err != nil {
		t.Fatalf("failed to create tmdb client: %v", err)
	}

	detail, err := client.SearchAndGetDetail(context.Background(), "movie", "沙丘", 2021)
	if err != nil {
		t.Fatalf("SearchAndGetDetail() error = %v", err)
	}

	if detail.TmdbID != 438631 {
		t.Errorf("TmdbID = %d, want 438631", detail.TmdbID)
	}
	if detail.Title != "沙丘" {
		t.Errorf("Title = %q, want 沙丘", detail.Title)
	}
	if detail.OriginalName != "Dune" {
		t.Errorf("OriginalName = %q, want Dune", detail.OriginalName)
	}
	if detail.Year != 2021 {
		t.Errorf("Year = %d, want 2021", detail.Year)
	}
	if detail.ReleaseDate != "2021-09-15" {
		t.Errorf("ReleaseDate = %q, want 2021-09-15", detail.ReleaseDate)
	}
	if detail.Rating != 7.8 {
		t.Errorf("Rating = %f, want 7.8", detail.Rating)
	}
	if detail.Director != "丹尼斯·维伦纽瓦" {
		t.Errorf("Director = %q, want 丹尼斯·维伦纽瓦", detail.Director)
	}
	if detail.Actors != "提莫西·查拉梅、丽贝卡·弗格森" {
		t.Errorf("Actors = %q, want 提莫西·查拉梅、丽贝卡·弗格森", detail.Actors)
	}
	if detail.PosterURL != "https://image.tmdb.org/t/p/w500/d5NXSklXo0qyIYkgV94XAgMIckC.jpg" {
		t.Errorf("PosterURL = %q", detail.PosterURL)
	}
}

func TestTmdbClient_LanguageFallback(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/3/tv/999", func(w http.ResponseWriter, r *http.Request) {
		lang := r.URL.Query().Get("language")
		w.Header().Set("Content-Type", "application/json")
		if lang == "zh-CN" {
			// 中文返回标题与简介为空
			fmt.Fprintln(w, `{
				"id": 999,
				"name": "",
				"original_name": "English Show",
				"overview": "",
				"first_air_date": "2023-01-01",
				"credits": {"cast": [], "crew": []}
			}`)
		} else {
			// 英文补全
			fmt.Fprintln(w, `{
				"id": 999,
				"name": "English Show",
				"original_name": "English Show",
				"overview": "This is an english show overview.",
				"first_air_date": "2023-01-01",
				"credits": {
					"cast": [{"name": "Actor John"}],
					"crew": [{"name": "Director Mike", "job": "Director"}]
				}
			}`)
		}
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client, err := NewTmdbClientWithHTTPClient("dummy-key", server.URL, server.Client())
	if err != nil {
		t.Fatalf("failed to create tmdb client: %v", err)
	}

	detail, err := client.GetDetailByID(context.Background(), "tv", 999)
	if err != nil {
		t.Fatalf("GetDetailByID() error = %v", err)
	}

	if detail.Title != "English Show" {
		t.Errorf("expected Title fallback to English Show, got %q", detail.Title)
	}
	if detail.Overview != "This is an english show overview." {
		t.Errorf("expected Overview fallback, got %q", detail.Overview)
	}
	if detail.Director != "Director Mike" {
		t.Errorf("expected Director fallback to Director Mike, got %q", detail.Director)
	}
	if detail.Actors != "Actor John" {
		t.Errorf("expected Actors fallback to Actor John, got %q", detail.Actors)
	}
}

func TestTmdbClient_Retry429(t *testing.T) {
	var attempts int32
	mux := http.NewServeMux()

	mux.HandleFunc("/3/movie/100", func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&attempts, 1)
		if current <= 2 {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{
			"id": 100,
			"title": "测试影片",
			"overview": "测试影片简介",
			"release_date": "2020-01-01"
		}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client, err := NewTmdbClientWithHTTPClient("dummy-key", server.URL, server.Client())
	if err != nil {
		t.Fatalf("failed to create tmdb client: %v", err)
	}

	detail, err := client.GetDetailByID(context.Background(), "movie", 100)
	if err != nil {
		t.Fatalf("expected success after retry, got error = %v", err)
	}

	if detail.TmdbID != 100 {
		t.Errorf("TmdbID = %d, want 100", detail.TmdbID)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestTmdbClient_MismatchDetection(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/3/search/movie", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{
			"page": 1,
			"results": [
				{
					"id": 12345,
					"title": "完全无关电影XYZ",
					"release_date": "2010-01-01"
				}
			]
		}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client, err := NewTmdbClientWithHTTPClient("dummy-key", server.URL, server.Client())
	if err != nil {
		t.Fatalf("failed to create tmdb client: %v", err)
	}

	// 搜索 2024 年的流浪地球，返回 2010 年的无关电影，应判定为不匹配
	_, err = client.SearchAndGetDetail(context.Background(), "movie", "流浪地球", 2024)
	if err != ErrTmdbMismatch {
		t.Fatalf("expected ErrTmdbMismatch, got %v", err)
	}
}
