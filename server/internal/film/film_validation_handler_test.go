package film

import (
	"net/http"
	"server/internal/model/dto"
	"server/internal/testutil"
	"testing"
)

func TestFilmHandler_FilmDelete_Validation(t *testing.T) {
	c, w := testutil.NewJSONContext(http.MethodPost, "/api/manage/film/delete", "invalid-json")
	FilmHd.FilmDelete(c)
	resp := testutil.DecodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on invalid JSON, got %d", resp.Code)
	}
	c, w = testutil.NewJSONContext(http.MethodPost, "/api/manage/film/delete", `{"ids":[]}`)
	FilmHd.FilmDelete(c)
	resp = testutil.DecodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty ids, got %d", resp.Code)
	}
}

func TestFilmHandler_FilmRefresh_Validation(t *testing.T) {
	c, w := testutil.NewJSONContext(http.MethodPost, "/api/manage/film/refresh", "invalid-json")
	FilmHd.FilmRefresh(c)
	resp := testutil.DecodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on invalid JSON, got %d", resp.Code)
	}
	c, w = testutil.NewJSONContext(http.MethodPost, "/api/manage/film/refresh", `{"ids":[]}`)
	FilmHd.FilmRefresh(c)
	resp = testutil.DecodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty ids, got %d", resp.Code)
	}
}
