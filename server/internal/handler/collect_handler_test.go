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
		&model.SlaveMoviePlaylist{},
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
		Grade:  model.SlaveCollect,
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
			Grade:  model.SlaveCollect,
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
			Grade:  model.SlaveCollect,
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
