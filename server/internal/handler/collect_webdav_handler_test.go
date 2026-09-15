package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	db.Mdb = gdb
	_ = gdb.AutoMigrate(model.AllModels...)
	return gdb
}

func TestCollectWebdavHandler_Report(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gdb := setupTestDB(t)

	// 1. 无 sourceId -> 失败
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/api/manage/collect/webdav/report", nil)
	CollectHd.WebdavReport(c)

	var resp dto.Response
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty sourceId, got %d", resp.Code)
	}

	// 2. 插入 WebDAV 源与扫描报告及项目
	source := model.FilmSource{
		Id:         "src_webdav_rep",
		Name:       "报告测试源",
		SourceType: model.SourceTypeWebdav,
		Grade:      model.SlaveCollect,
		State:      true,
		Uri:        "webdav://127.0.0.1:8080/dav",
	}
	gdb.Create(&source)

	report := model.WebdavScanReport{
		SourceId: source.Id,
		Found:    10,
		Parsed:   8,
		Status:   "done",
	}
	gdb.Create(&report)

	item := model.WebdavScanItem{
		SourceId: source.Id,
		PathHash: "p_hash_1",
		RelPath:  "Movie/Test.mp4",
		Status:   "unmatched",
	}
	gdb.Create(&item)

	// 正常查询
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/api/manage/collect/webdav/report?sourceId=src_webdav_rep", nil)
	CollectHd.WebdavReport(c)

	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected SUCCESS, got %d msg=%s", resp.Code, resp.Msg)
	}
}

func TestCollectWebdavHandler_BindAndRescrape_Validation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupTestDB(t)

	// 1. WebdavBind 参数不完整
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(model.WebdavBindRequest{
		SourceId: "",
	})
	c.Request, _ = http.NewRequest(http.MethodPost, "/api/manage/collect/webdav/bind", bytes.NewReader(body))
	CollectHd.WebdavBind(c)

	var resp dto.Response
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on invalid bind req, got %d", resp.Code)
	}

	// 2. WebdavRescrape 缺少 sourceId
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	rescrapeBody, _ := json.Marshal(model.WebdavRescrapeRequest{
		SourceId: "",
	})
	c.Request, _ = http.NewRequest(http.MethodPost, "/api/manage/collect/webdav/rescrape", bytes.NewReader(rescrapeBody))
	CollectHd.WebdavRescrape(c)

	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty rescrape sourceId, got %d", resp.Code)
	}
}
