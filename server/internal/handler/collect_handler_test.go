package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupCollectHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := gdb.AutoMigrate(
		&model.FilmSource{},
		&model.FilmSourcePlaylist{},
		&model.CollectSourceStats{},
		&model.Category{},
		&model.SourceCategory{},
		&model.CategoryMapping{},
		&model.FailureRecord{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	db.Mdb = gdb
	return gdb
}

func TestCollectHandler_FilmSourceUpdate_DecoupledMetadataUpdate(t *testing.T) {
	gdb := setupCollectHandlerTestDB(t)

	// 初始站点使用一个无法连接的无效 URI
	unreachableURI := "http://127.0.0.1:59999/unreachable/api.php"
	src := model.FilmSource{
		Id:     "src_unreachable",
		Name:   "原始站点",
		Uri:    unreachableURI,
		Sort:   0,
		Format: model.SourceFormatJSON,
		State:  true,
	}
	if err := gdb.Create(&src).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}

	h := &CollectHandler{}

	// 1. 仅修改站点名称（URI 与 Format 未改变）
	// 即使远端接口完全无法连通，也应该成功保存，不触发接口测试
	updateBody := filmSourceBody{
		FilmSource: model.FilmSource{
			Id:     "src_unreachable",
			Name:   "新站点",
			Uri:    unreachableURI,
			Sort:   0,
			Format: model.SourceFormatJSON,
			State:  true,
		},
	}
	bodyBytes, _ := json.Marshal(updateBody)
	c, w := testContext(http.MethodPost, "/api/manage/collect/update")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/manage/collect/update", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")

	h.FilmSourceUpdate(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected update to succeed when URI unchanged even if unreachable, got code=%d msg=%s", resp.Code, resp.Msg)
	}

	// 验证数据库名称已更新
	var updated model.FilmSource
	if err := gdb.First(&updated, "id = ?", "src_unreachable").Error; err != nil {
		t.Fatalf("query updated source: %v", err)
	}
	if updated.Name != "新站点" {
		t.Fatalf("expected updated name '新站点', got '%s'", updated.Name)
	}

	// 2. 修改 URI 指向另一个无法连接的地址 -> 此时必须触发接口探测并报错拦截
	newUnreachableURI := "http://127.0.0.1:59998/new_unreachable/api.php"
	updateBodyUriChanged := filmSourceBody{
		FilmSource: model.FilmSource{
			Id:     "src_unreachable",
			Name:   "修改地址",
			Uri:    newUnreachableURI,
			Sort:   0,
			Format: model.SourceFormatJSON,
			State:  true,
		},
	}
	bodyBytes2, _ := json.Marshal(updateBodyUriChanged)
	c2, w2 := testContext(http.MethodPost, "/api/manage/collect/update")
	c2.Request = httptest.NewRequest(http.MethodPost, "/api/manage/collect/update", bytes.NewReader(bodyBytes2))
	c2.Request.Header.Set("Content-Type", "application/json")

	h.FilmSourceUpdate(c2)
	resp2 := decodeResponse(t, w2)
	if resp2.Code == dto.SUCCESS {
		t.Fatal("expected update to fail when changing URI to an unreachable address")
	}
}

func TestCollectHandler_FilmSourceSyncCategory_Validations(t *testing.T) {
	_ = setupCollectHandlerTestDB(t)
	h := &CollectHandler{}

	// 1. 空 ID 校验
	c, w := testContext(http.MethodPost, "/api/manage/collect/category/sync")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/manage/collect/category/sync", bytes.NewReader([]byte(`{"id":""}`)))
	c.Request.Header.Set("Content-Type", "application/json")
	h.FilmSourceSyncCategory(c)
	resp := decodeResponse(t, w)
	if resp.Code == dto.SUCCESS {
		t.Fatal("expected failure for empty source ID")
	}

	// 2. 站点不存在校验
	c2, w2 := testContext(http.MethodPost, "/api/manage/collect/category/sync")
	c2.Request = httptest.NewRequest(http.MethodPost, "/api/manage/collect/category/sync", bytes.NewReader([]byte(`{"id":"non_existent"}`)))
	c2.Request.Header.Set("Content-Type", "application/json")
	h.FilmSourceSyncCategory(c2)
	resp2 := decodeResponse(t, w2)
	if resp2.Code == dto.SUCCESS {
		t.Fatal("expected failure for non-existent source")
	}
}

func TestCollectHandler_FilmSourceAdd_AsyncCategorySync(t *testing.T) {
	gdb := setupCollectHandlerTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"msg":"数据列表","page":1,"pagecount":1,"limit":"20","total":0,"list":[],"class":[]}`))
	}))
	defer server.Close()

	h := &CollectHandler{}
	body := filmSourceBody{
		FilmSource: model.FilmSource{
			Name:   "测试新站",
			Uri:    server.URL,
			Format: model.SourceFormatJSON,
			State:  true,
		},
	}
	bodyBytes, _ := json.Marshal(body)
	c, w := testContext(http.MethodPost, "/api/manage/collect/add")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/manage/collect/add", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")

	h.FilmSourceAdd(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected add to succeed immediately, got code=%d msg=%s", resp.Code, resp.Msg)
	}

	var count int64
	if err := gdb.Model(&model.FilmSource{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("expected 1 source saved, got count=%d, err=%v", count, err)
	}
}
