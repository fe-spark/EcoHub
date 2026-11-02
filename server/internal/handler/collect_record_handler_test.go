package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	if err := gdb.AutoMigrate(&model.CollectRecord{}); err != nil {
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
