package film

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"server/internal/film/snapshot"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/testutil"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupFilmHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(
		&model.FilmSource{}, &model.FilmIndex{}, &model.FilmSourcePlaylist{},
		&model.Category{}, &model.SourceCategory{}, &model.CategoryMapping{},
		&model.MovieSourceMapping{}, &model.MovieMatchKey{}, &model.BannerSlot{},
		&model.PrimaryState{}, &model.SiteConfigRecord{},
	); err != nil {
		t.Fatal(err)
	}
	prevM, prevR := db.Mdb, db.Rdb
	db.Mdb, db.Rdb = gdb, nil
	snapshot.ClearActiveFilmReadModel()
	snapshot.ClearActiveSnapshotVersion()
	t.Cleanup(func() {
		db.Mdb, db.Rdb = prevM, prevR
		snapshot.ClearActiveFilmReadModel()
		snapshot.ClearActiveSnapshotVersion()
	})
	return gdb
}

func testFilmContext(method, path string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, nil)
	return c, w
}

func testFilmJSONContext(method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	c, w := testFilmContext(method, path)
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, w
}

func decodeFilmResponse(t *testing.T, w *httptest.ResponseRecorder) dto.Response {
	t.Helper()
	var resp dto.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v body=%s", err, w.Body.String())
	}
	return resp
}

func TestFilmSearchOptions_Success(t *testing.T) {
	setupFilmHandlerTestDB(t)
	c, w := testFilmContext(http.MethodGet, "/api/manage/film/search/options?sourceId=")
	FilmHd.FilmSearchOptions(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	resp := decodeFilmResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("code=%d want %d", resp.Code, dto.SUCCESS)
	}
}

func TestFilmDelete_NonPrimarySourceReturnsMismatch(t *testing.T) {
	gdb := setupFilmHandlerTestDB(t)
	if err := gdb.Create(&[]model.FilmSource{
		{Id: "src1", Name: "主站", Uri: "http://src1.test", State: true, Sort: 0},
		{Id: "src2", Name: "副站", Uri: "http://src2.test", State: true, Sort: 1},
	}).Error; err != nil {
		t.Fatal(err)
	}
	c, w := testFilmJSONContext(http.MethodPost, "/api/manage/film/delete", `{"ids":[201],"sourceId":"src2"}`)
	FilmHd.FilmDelete(c)
	if w.Code != http.StatusConflict || decodeFilmResponse(t, w).Code != dto.PrimaryMismatchCode {
		t.Fatalf("non-primary delete want 409 PRIMARY_MISMATCH, got status=%d body=%s", w.Code, w.Body.String())
	}
	c, w = testFilmJSONContext(http.MethodPost, "/api/manage/film/refresh", `{"ids":[201],"sourceId":"src2"}`)
	FilmHd.FilmRefresh(c)
	if w.Code != http.StatusConflict || decodeFilmResponse(t, w).Code != dto.PrimaryMismatchCode {
		t.Fatalf("non-primary refresh want 409 PRIMARY_MISMATCH, got status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestFilmSearchPage_IncludesIndexingState(t *testing.T) {
	cInit, _ := testFilmContext(http.MethodGet, "/api/manage/film/search/list?sourceId=")
	FilmHd.FilmSearchPage(cInit)

	snapshot.NotifySearchIndexProgress(100, 45, "indexing")
	defer snapshot.NotifySearchIndexProgress(0, 0, "idle")

	c, w := testFilmContext(http.MethodGet, "/api/manage/film/search/list?sourceId=")
	FilmHd.FilmSearchPage(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	resp := decodeFilmResponse(t, w)
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

func TestFilmHandler_FilmDetail_AllowsHiddenCategory(t *testing.T) {
	gdb := setupFilmHandlerTestDB(t)
	if err := gdb.Create(&model.Category{Id: 10, Pid: 0, Name: "隐藏大类", Show: false, Sort: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 501, FirstSourceId: "src1"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 10, Cid: 10, CName: "隐藏大类"},
		FilmIndexContent:  model.FilmIndexContent{Name: "隐藏分类影片"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	c, w := testFilmContext(http.MethodGet, "/api/manage/film/detail?id=501")
	FilmHd.FilmDetail(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200 body=%s", w.Code, w.Body.String())
	}
	resp := decodeFilmResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("code=%d want %d msg=%s", resp.Code, dto.SUCCESS, resp.Msg)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", resp.Data)
	}
	detail, ok := data["detail"].(map[string]any)
	if !ok || detail["name"] != "隐藏分类影片" {
		t.Fatalf("unexpected detail=%v", data["detail"])
	}
	if isManual, ok := data["isManualFilm"].(bool); !ok || isManual {
		t.Fatalf("expected collected film isManualFilm=false, got %v", data["isManualFilm"])
	}
}
