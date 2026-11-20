package handler

import (
	"net/http"
	"testing"

	"server/internal/model"
	"server/internal/model/dto"
	filmsnapshot "server/internal/repository/film/snapshot"
)

func TestFilmSearchOptions_Success(t *testing.T) {
	// 需要可用的库；原先靠别的用例泄漏的 db.Mdb 才能通过。
	setupCollectHandlerTestDB(t)
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

func TestFilmDelete_NonPrimarySourceReturnsMismatch(t *testing.T) {
	gdb := setupCollectHandlerTestDB(t)
	if err := gdb.Create(&[]model.FilmSource{
		{Id: "src1", Name: "主站", Uri: "http://src1.test", State: true, Sort: 0},
		{Id: "src2", Name: "副站", Uri: "http://src2.test", State: true, Sort: 1},
	}).Error; err != nil {
		t.Fatal(err)
	}
	c, w := testJSONContext(http.MethodPost, "/api/manage/film/delete", `{"ids":[201],"sourceId":"src2"}`)
	FilmHd.FilmDelete(c)
	if w.Code != http.StatusConflict || decodeResponse(t, w).Code != dto.PrimaryMismatchCode {
		t.Fatalf("non-primary delete want 409 PRIMARY_MISMATCH, got status=%d body=%s", w.Code, w.Body.String())
	}
	c, w = testJSONContext(http.MethodPost, "/api/manage/film/refresh", `{"ids":[201],"sourceId":"src2"}`)
	FilmHd.FilmRefresh(c)
	if w.Code != http.StatusConflict || decodeResponse(t, w).Code != dto.PrimaryMismatchCode {
		t.Fatalf("non-primary refresh want 409 PRIMARY_MISMATCH, got status=%d body=%s", w.Code, w.Body.String())
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
