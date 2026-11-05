package handler

import (
	"net/http"
	"testing"

	"server/internal/model/dto"
	filmsnapshot "server/internal/repository/film/snapshot"
)

func TestFilmSearchOptions_Success(t *testing.T) {
	c, w := testContext(http.MethodGet, "/api/manage/film/search/options?sourceId=")
	FilmHd.FilmSearchOptions(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	resp := decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("code=%d want %d", resp.Code, dto.SUCCESS)
	}
}

func TestFilmSearchPage_IncludesIndexingState(t *testing.T) {
	cInit, _ := testContext(http.MethodGet, "/api/manage/film/search/list?sourceId=")
	FilmHd.FilmSearchPage(cInit)

	filmsnapshot.NotifySearchIndexProgress(100, 45, "indexing")
	defer filmsnapshot.NotifySearchIndexProgress(0, 0, "idle")

	c, w := testContext(http.MethodGet, "/api/manage/film/search/list?sourceId=")
	FilmHd.FilmSearchPage(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	resp := decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("code=%d want %d msg=%s body=%s", resp.Code, dto.SUCCESS, resp.Msg, w.Body.String())
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any data, got %T", resp.Data)
	}
	indexing, ok := data["indexing"].(bool)
	if !ok || !indexing {
		t.Fatalf("expected indexing=true, got %v (data=%v)", data["indexing"], data)
	}
	prog, ok := data["indexingProgress"].(map[string]any)
	if !ok {
		t.Fatalf("expected indexingProgress map, got %T", data["indexingProgress"])
	}
	if prog["status"] != "indexing" {
		t.Fatalf("expected status=indexing, got %v", prog["status"])
	}
}
