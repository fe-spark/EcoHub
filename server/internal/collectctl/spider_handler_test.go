package collectctl

import (
	"bytes"
	"encoding/json"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net/http"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/testutil"
	"testing"
)

func TestSpiderHandler_InventoryStats(t *testing.T) {
	orig := db.Mdb
	db.Mdb = nil
	t.Cleanup(func() { db.Mdb = orig })

	c, w := testutil.NewTestContext(http.MethodGet, "/api/manage/spider/clear/stats")
	SpiderHd.InventoryStats(c)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := testutil.DecodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected code FAILED, got %d msg=%s", resp.Code, resp.Msg)
	}
}

func TestSpiderHandler_InventoryStats_ReadyAndRefresh(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.FilmIndex{}, &model.Category{}, &model.CategoryMapping{}, &model.CollectRecord{}, &model.PrimaryState{}); err != nil {
		t.Fatal(err)
	}
	orig := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = orig })

	gdb.Create(&model.FilmSource{Id: "src_1", Name: "主站", Uri: "http://a.test", State: true, Sort: 0})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, FirstSourceId: "src_1"},
		FilmIndexContent:  model.FilmIndexContent{Name: "片1"},
	})

	c1, w1 := testutil.NewTestContext(http.MethodGet, "/api/manage/spider/clear/stats")
	SpiderHd.InventoryStats(c1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w1.Code)
	}
	var payload1 struct {
		Code int `json:"code"`
		Data struct {
			Status  string `json:"status"`
			Library struct {
				Films int64 `json:"films"`
			} `json:"library"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w1.Body.Bytes(), &payload1); err != nil {
		t.Fatal(err)
	}
	if payload1.Code != dto.SUCCESS || payload1.Data.Status != "ready" || payload1.Data.Library.Films != 1 {
		t.Fatalf("unexpected response: %+v", payload1)
	}

	// 已出结果后未触发事件时，不传 refresh 直接复用内存结果（仍为 1），传 refresh=1 立即重算（为 2）
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, FirstSourceId: "src_1"},
		FilmIndexContent:  model.FilmIndexContent{Name: "片2"},
	})
	c1b, w1b := testutil.NewTestContext(http.MethodGet, "/api/manage/spider/clear/stats")
	SpiderHd.InventoryStats(c1b)
	var payload1b struct {
		Code int `json:"code"`
		Data struct {
			Status  string `json:"status"`
			Library struct {
				Films int64 `json:"films"`
			} `json:"library"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w1b.Body.Bytes(), &payload1b); err != nil {
		t.Fatal(err)
	}
	if payload1b.Code != dto.SUCCESS || payload1b.Data.Status != "ready" || payload1b.Data.Library.Films != 1 {
		t.Fatalf("expected memory result Films=1 without refresh, got %+v", payload1b)
	}
	c2, w2 := testutil.NewTestContext(http.MethodGet, "/api/manage/spider/clear/stats?refresh=1")
	SpiderHd.InventoryStats(c2)
	var payload2 struct {
		Code int `json:"code"`
		Data struct {
			Status  string `json:"status"`
			Library struct {
				Films int64 `json:"films"`
			} `json:"library"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &payload2); err != nil {
		t.Fatal(err)
	}
	if payload2.Code != dto.SUCCESS || payload2.Data.Status != "ready" || payload2.Data.Library.Films != 2 {
		t.Fatalf("unexpected refreshed response: %+v", payload2)
	}
}

func TestSpiderHandler_StarSpiderAndStopTask_BatchContract(t *testing.T) {
	// 1. 测试 StarSpider 参数校验（空的 ids 或 0 时间）
	c, w := testutil.NewTestContext(http.MethodPost, "/api/manage/spider/start")
	body, _ := json.Marshal(model.CollectParams{Ids: []string{}, Time: 0})
	c.Request, _ = http.NewRequest(http.MethodPost, "/api/manage/spider/start", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	SpiderHd.StarSpider(c)
	resp := testutil.DecodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on zero time, got %d", resp.Code)
	}

	c, w = testutil.NewTestContext(http.MethodPost, "/api/manage/spider/start")
	body, _ = json.Marshal(model.CollectParams{Ids: []string{}, Time: 24})
	c.Request, _ = http.NewRequest(http.MethodPost, "/api/manage/spider/start", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	SpiderHd.StarSpider(c)
	resp = testutil.DecodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty ids, got %d", resp.Code)
	}

	// 2. 测试 StopTask 参数校验（空的 ids）
	c, w = testutil.NewTestContext(http.MethodPost, "/api/manage/spider/stop")
	body, _ = json.Marshal(map[string][]string{"ids": {}})
	c.Request, _ = http.NewRequest(http.MethodPost, "/api/manage/spider/stop", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	SpiderHd.StopTask(c)
	resp = testutil.DecodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty ids for StopTask, got %d", resp.Code)
	}
}
