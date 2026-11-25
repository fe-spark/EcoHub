package film

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"encoding/json"

	"server/internal/model/dto"

	"github.com/gin-gonic/gin"
)

func testJSONContext(method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, w
}

func decodeResponse(t *testing.T, w *httptest.ResponseRecorder) dto.Response {
	t.Helper()
	var resp dto.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v body=%s", err, w.Body.String())
	}
	return resp
}

func TestFilmHandler_FilmDelete_Validation(t *testing.T) {
	c, w := testJSONContext(http.MethodPost, "/api/manage/film/delete", "invalid-json")
	FilmHd.FilmDelete(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on invalid JSON, got %d", resp.Code)
	}
	c, w = testJSONContext(http.MethodPost, "/api/manage/film/delete", `{"ids":[]}`)
	FilmHd.FilmDelete(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty ids, got %d", resp.Code)
	}
}

func TestFilmHandler_FilmRefresh_Validation(t *testing.T) {
	c, w := testJSONContext(http.MethodPost, "/api/manage/film/refresh", "invalid-json")
	FilmHd.FilmRefresh(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on invalid JSON, got %d", resp.Code)
	}
	c, w = testJSONContext(http.MethodPost, "/api/manage/film/refresh", `{"ids":[]}`)
	FilmHd.FilmRefresh(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty ids, got %d", resp.Code)
	}
}
