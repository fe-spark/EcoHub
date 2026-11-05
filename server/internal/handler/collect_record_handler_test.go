package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupHandlerTestDB(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.CollectRecord{}, &model.CollectRecordFilmItem{}, &model.FilmIndex{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })
}

func TestCollectRecordHandler_HistoryList(t *testing.T) {
	setupHandlerTestDB(t)

	_ = repository.CreateCollectRecord(&model.CollectRecord{
		BatchID:     "b-1",
		Trigger:     model.CollectTriggerManual,
		Status:      model.CollectRecordStatusSuccess,
		StartedAt:   time.Now(),
		SourceCount: 2,
	})

	r := gin.New()
	r.GET("/history/list", CollectRecordHd.HistoryList)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/history/list?current=1&pageSize=10", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Total int                   `json:"total"`
			List  []model.CollectRecord `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}
	if resp.Data.Total != 1 || len(resp.Data.List) != 1 {
		t.Fatalf("expected 1 record, got %+v", resp.Data)
	}
}

func TestCollectRecordHandler_DeleteAndClear(t *testing.T) {
	setupHandlerTestDB(t)

	rec1 := &model.CollectRecord{
		BatchID:   "b-del-1",
		Trigger:   model.CollectTriggerManual,
		Status:    model.CollectRecordStatusSuccess,
		StartedAt: time.Now(),
	}
	rec2 := &model.CollectRecord{
		BatchID:   "b-del-2",
		Trigger:   model.CollectTriggerCron,
		Status:    model.CollectRecordStatusSuccess,
		StartedAt: time.Now(),
	}
	_ = repository.CreateCollectRecord(rec1)
	_ = repository.CreateCollectRecord(rec2)

	r := gin.New()
	r.POST("/history/delete", CollectRecordHd.HistoryDelete)
	r.POST("/history/clear", CollectRecordHd.HistoryClear)

	// 测试删除单条
	delBody, _ := json.Marshal(map[string]any{"ids": []uint{rec1.ID}})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/history/delete", bytes.NewReader(delBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on delete, got %d", w.Code)
	}

	list, total, _ := repository.GetCollectRecordList(model.CollectRecordQuery{})
	if total != 1 || list[0].BatchID != "b-del-2" {
		t.Fatalf("expected only b-del-2 remaining, got %d, list=%+v", total, list)
	}

	// 测试全部清空
	wClear := httptest.NewRecorder()
	reqClear, _ := http.NewRequest("POST", "/history/clear", nil)
	r.ServeHTTP(wClear, reqClear)
	if wClear.Code != http.StatusOK {
		t.Fatalf("expected 200 on clear, got %d", wClear.Code)
	}

	_, totalAfterClear, _ := repository.GetCollectRecordList(model.CollectRecordQuery{})
	if totalAfterClear != 0 {
		t.Fatalf("expected 0 after clear, got %d", totalAfterClear)
	}
}

func TestCollectRecordHandler_HistoryStream(t *testing.T) {
	setupHandlerTestDB(t)

	r := gin.New()
	r.GET("/history/stream", CollectRecordHd.HistoryStream)

	// 1. 缺少 batchIds 返回错误
	wErr := httptest.NewRecorder()
	reqErr, _ := http.NewRequest("GET", "/history/stream", nil)
	r.ServeHTTP(wErr, reqErr)
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(wErr.Body.Bytes(), &resp)
	if resp.Code == 0 || !strings.Contains(resp.Msg, "缺少批次标识参数") {
		t.Fatalf("expected error when batchIds missing, got code=%d, msg=%s", resp.Code, resp.Msg)
	}

	// 2. 带 batchIds 正常建立 SSE 流
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", "/history/stream?batchIds=b-stream-1,b-stream-2", nil)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.ServeHTTP(w, req)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %s", ct)
	}
	if !strings.Contains(w.Body.String(), "record_snapshot") {
		t.Fatalf("expected record_snapshot event, got: %s", w.Body.String())
	}
}

func TestCollectRecordHandler_HistoryFilms(t *testing.T) {
	setupHandlerTestDB(t)

	db.Mdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 888},
		FilmIndexContent:  model.FilmIndexContent{Name: "流浪地球"},
	})
	_ = repository.BatchSaveCollectRecordFilms("b-film-1", map[string][]int64{
		"src_earth": {888},
	})
	_ = repository.CreateCollectRecord(&model.CollectRecord{
		BatchID:    "b-film-1",
		Trigger:    model.CollectTriggerManual,
		Status:     model.CollectRecordStatusSuccess,
		FilmCount:  1,
		DetailJson: `[{"sourceId":"src_earth","filmsTotal":1}]`,
		StartedAt:  time.Now(),
	})

	r := gin.New()
	r.GET("/history/films", CollectRecordHd.HistoryFilms)

	// 缺少批次号
	wErr := httptest.NewRecorder()
	reqErr, _ := http.NewRequest("GET", "/history/films", nil)
	r.ServeHTTP(wErr, reqErr)
	var respErr struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(wErr.Body.Bytes(), &respErr)
	if respErr.Code == 0 || !strings.Contains(respErr.Msg, "缺少批次号参数") {
		t.Fatalf("expected missing batchId error, got code=%d, msg=%s", respErr.Code, respErr.Msg)
	}

	// 正常查询
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/history/films?batchId=b-film-1&sourceId=src_earth", nil)
	r.ServeHTTP(w, req)
	var resp struct {
		Code int `json:"code"`
		Data struct {
			List  []model.CollectRecordFilm `json:"list"`
			Total int64                     `json:"total"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != 0 || resp.Data.Total != 1 || len(resp.Data.List) != 1 {
		t.Fatalf("expected 1 film, got code=%d, data=%+v", resp.Code, resp.Data)
	}
	if resp.Data.List[0].Name != "流浪地球" {
		t.Fatalf("expected 流浪地球, got %s", resp.Data.List[0].Name)
	}
}
