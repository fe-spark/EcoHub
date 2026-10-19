package handler

import (
	"net/http"
	"testing"

	"server/internal/model/dto"
)

func TestTMDBHandler_Scrape_Validation(t *testing.T) {
	// 1. 无效 JSON
	c, w := testJSONContext(http.MethodPost, "/api/manage/tmdb/scrape", "invalid-json")
	TMDBHd.Scrape(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on invalid JSON, got %d", resp.Code)
	}

	// 2. 缺少 ids 参数
	c, w = testJSONContext(http.MethodPost, "/api/manage/tmdb/scrape", `{"ids":[]}`)
	TMDBHd.Scrape(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty ids, got %d", resp.Code)
	}
}

func TestFilmHandler_FilmDelete_Validation(t *testing.T) {
	// 1. 无效 JSON
	c, w := testJSONContext(http.MethodPost, "/api/manage/film/search/del", "invalid-json")
	FilmHd.FilmDelete(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on invalid JSON, got %d", resp.Code)
	}

	// 2. 缺少 ids 参数
	c, w = testJSONContext(http.MethodPost, "/api/manage/film/search/del", `{"ids":[]}`)
	FilmHd.FilmDelete(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty ids, got %d", resp.Code)
	}
}

func TestSpiderHandler_UpdateSpider_Validation(t *testing.T) {
	// 1. 无效 JSON
	c, w := testJSONContext(http.MethodPost, "/api/manage/spider/update", "invalid-json")
	SpiderHd.UpdateSpider(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on invalid JSON, got %d", resp.Code)
	}

	// 2. 缺少 ids 参数
	c, w = testJSONContext(http.MethodPost, "/api/manage/spider/update", `{"ids":[]}`)
	SpiderHd.UpdateSpider(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty ids, got %d", resp.Code)
	}
}
