package handler

import (
	"net/http"
	"testing"

	"server/internal/model/dto"
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
