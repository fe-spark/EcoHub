package handler

import (
	"bytes"
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

func TestCollectRecordHandler_HistoryRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	_ = gdb.AutoMigrate(&model.CollectRecord{}, &model.FilmSource{}, &model.CategoryMapping{}, &model.SourceCategory{})
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	src1 := model.FilmSource{Id: "retry-s1", Name: "失败源1", State: true}
	_ = gdb.Create(&src1).Error
	_ = gdb.Create(&model.SourceCategory{SourceId: src1.Id, SourceTypeId: 1, RawName: "电影"}).Error
	_ = gdb.Create(&model.CategoryMapping{SourceId: src1.Id, SourceTypeId: 1, CategoryId: 1}).Error

	detailJson := `[{"sourceId":"retry-s1","sourceName":"失败源1","status":"failed","error":"连接超时"}]`
	rec := &model.CollectRecord{
		BatchID:     "b-orig-1",
		Trigger:     model.CollectTriggerManual,
		Status:      model.CollectRecordStatusFailed,
		StartedAt:   time.Now(),
		DetailJson:  detailJson,
		SourceCount: 1,
	}
	_ = repository.CreateCollectRecord(rec)

	r := gin.New()
	r.POST("/history/retry", CollectRecordHd.HistoryRetry)

	reqBody, _ := json.Marshal(model.CollectRecordRetryReq{
		BatchID: "b-orig-1",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/history/retry", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Code int                      `json:"code"`
		Msg  string                   `json:"msg"`
		Data model.CollectBatchResult `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d, msg=%s", resp.Code, resp.Msg)
	}
	if len(resp.Data.Started) != 1 || resp.Data.Started[0].Id != "retry-s1" {
		t.Fatalf("expected retry-s1 to start, got %+v", resp.Data)
	}
	if resp.Data.BatchID == "" {
		t.Fatal("expected retry batchId")
	}
	saved, err := repository.GetCollectRecordByBatchID(resp.Data.BatchID)
	if err != nil || saved == nil {
		t.Fatalf("retry record missing: %v", err)
	}
	if saved.ParentBatchID != "b-orig-1" || saved.Trigger != model.CollectTriggerRetry {
		t.Fatalf("unexpected retry record: %+v", saved)
	}

	// 测试 2: 传入非本批次失败的 SourceIDs，应直接报错
	reqBodyInvalid, _ := json.Marshal(model.CollectRecordRetryReq{
		BatchID:   "b-orig-1",
		SourceIDs: []string{"not-in-batch"},
	})
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("POST", "/history/retry", bytes.NewReader(reqBodyInvalid))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)
	var resp2 struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(w2.Body.Bytes(), &resp2)
	if resp2.Code == 0 {
		t.Fatalf("expected error for non-matching sourceIds, got code 0: %+v", resp2)
	}

	// 测试 3: 当目标站点被禁用时，无法启动应返回明确错误，不能静默报成功
	_ = gdb.Model(&model.FilmSource{}).Where("id = ?", "retry-s1").Update("state", false)
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("POST", "/history/retry", bytes.NewReader(reqBody))
	req3.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w3, req3)
	var resp3 struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(w3.Body.Bytes(), &resp3)
	if resp3.Code == 0 {
		t.Fatalf("expected error when source disabled, got code 0: %+v", resp3)
	}
}

func TestCollectRecordHandler_HistoryRetryBadDetail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	_ = gdb.AutoMigrate(&model.CollectRecord{})
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	_ = repository.CreateCollectRecord(&model.CollectRecord{
		BatchID:    "b-bad-json",
		Trigger:    model.CollectTriggerManual,
		Status:     model.CollectRecordStatusFailed,
		StartedAt:  time.Now(),
		DetailJson: "{",
	})

	r := gin.New()
	r.POST("/history/retry", CollectRecordHd.HistoryRetry)
	reqBody, _ := json.Marshal(model.CollectRecordRetryReq{BatchID: "b-bad-json"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/history/retry", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code == 0 || !strings.Contains(resp.Msg, "批次明细损坏") {
		t.Fatalf("expected 批次明细损坏, got %+v", resp)
	}
}
