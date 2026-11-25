package collectctl

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"server/internal/collect/progress"
	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/sourcecatalog"
	"server/internal/testutil"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func testContext(method, path string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, nil)
	return c, w
}

func testJSONContext(method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
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

func setupCollectHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := gdb.AutoMigrate(model.AllModels...); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() {
		// 启停/改站会触发切主站预热、异步同步分类，它们读全局 db；等它们结束再还原，避免 -race 报竞争。
		if !WaitAsyncWork(15 * time.Second) {
			t.Error("service background work did not finish")
		}
		db.Mdb = prev
	})
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
	c, w := testContext(http.MethodPost, "/api/manage/collect/save")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/manage/collect/save", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")

	h.FilmSourceSave(c)
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

	// 2. 修改 URI 指向另一个地址 -> 不进行同步阻塞探测，直接保存成功并通过异步处理分类
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
	c2, w2 := testContext(http.MethodPost, "/api/manage/collect/save")
	c2.Request = httptest.NewRequest(http.MethodPost, "/api/manage/collect/save", bytes.NewReader(bodyBytes2))
	c2.Request.Header.Set("Content-Type", "application/json")

	h.FilmSourceSave(c2)
	resp2 := decodeResponse(t, w2)
	if resp2.Code != dto.SUCCESS {
		t.Fatalf("expected update to succeed immediately without blocking network probe, got code=%d msg=%s", resp2.Code, resp2.Msg)
	}

	var updated2 model.FilmSource
	if err := gdb.First(&updated2, "id = ?", "src_unreachable").Error; err != nil {
		t.Fatalf("query updated source: %v", err)
	}
	if updated2.Uri != newUnreachableURI {
		t.Fatalf("expected updated uri '%s', got '%s'", newUnreachableURI, updated2.Uri)
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
	c, w := testContext(http.MethodPost, "/api/manage/collect/save")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/manage/collect/save", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")

	h.FilmSourceSave(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected add to succeed immediately, got code=%d msg=%s", resp.Code, resp.Msg)
	}

	var count int64
	if err := gdb.Model(&model.FilmSource{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("expected 1 source saved, got count=%d, err=%v", count, err)
	}
}

func TestCollectHandler_StateBatchAndRecordOperations(t *testing.T) {
	gdb := setupCollectHandlerTestDB(t)

	src1 := model.FilmSource{Id: "src_s1", Name: "站点1", Uri: "http://127.0.0.1:59991", State: false}
	src2 := model.FilmSource{Id: "src_s2", Name: "站点2", Uri: "http://127.0.0.1:59992", State: false}
	gdb.Create(&src1)
	gdb.Create(&src2)

	h := &CollectHandler{}

	// 1. 批量/单站更新状态: 启用 src_s1 与 src_s2
	stateReq := model.FilmSourceStateBatchRequest{
		Ids:   []string{"src_s1", "src_s2"},
		State: true,
	}
	bodyBytes, _ := json.Marshal(stateReq)
	c, w := testContext(http.MethodPost, "/api/manage/collect/state")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/manage/collect/state", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")
	h.FilmSourceBatchChange(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected state update success, got code=%d msg=%s", resp.Code, resp.Msg)
	}

	var s1 model.FilmSource
	gdb.First(&s1, "id = ?", "src_s1")
	if !s1.State {
		t.Fatalf("expected src_s1 state to be true")
	}
}

func TestCollectHandler_FilmSourceDelCancel_ValidatesJSONAndEmptyResult(t *testing.T) {
	_ = setupCollectHandlerTestDB(t)
	h := &CollectHandler{}

	// 1. 畸形 JSON 应报错拒绝，不能静默退化为全量取消
	c1, w1 := testContext(http.MethodPost, "/api/manage/collect/delete/cancel")
	c1.Request = httptest.NewRequest(http.MethodPost, "/api/manage/collect/delete/cancel", bytes.NewReader([]byte(`{"ids":"invalid"}`)))
	c1.Request.Header.Set("Content-Type", "application/json")
	h.FilmSourceDelCancel(c1)
	resp1 := decodeResponse(t, w1)
	if resp1.Code == dto.SUCCESS {
		t.Fatalf("expected failure for invalid JSON body, got success: %+v", resp1)
	}

	// 2. 空对象传参且当前无排队站时，返回明确提示文案
	c2, w2 := testContext(http.MethodPost, "/api/manage/collect/delete/cancel")
	c2.Request = httptest.NewRequest(http.MethodPost, "/api/manage/collect/delete/cancel", bytes.NewReader([]byte(`{}`)))
	c2.Request.Header.Set("Content-Type", "application/json")
	h.FilmSourceDelCancel(c2)
	resp2 := decodeResponse(t, w2)
	if resp2.Code != dto.SUCCESS || resp2.Msg != "当前无排队中的待取消站点" {
		t.Fatalf("unexpected response for empty cancel queue: %+v", resp2)
	}
}

func TestCollectHandler_PrimaryHandoverRequiresAdminAndCheckAllSkipsPrimary(t *testing.T) {
	gdb := setupCollectHandlerTestDB(t)
	if err := gdb.Create(&model.FilmSource{
		Id:    "src_p",
		Name:  "主站",
		Uri:   "http://127.0.0.1:59995/p",
		State: true,
		Sort:  0,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{
		Id:    "src_s",
		Name:  "备选站",
		Uri:   "http://127.0.0.1:59996/s",
		State: true,
		Sort:  1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{
		Id:           "src_proxy",
		Name:         "代理站",
		Uri:          "http://127.0.0.1:59993/proxy",
		State:        true,
		Sort:         2,
		ProxyCollect: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	_ = sourcecatalog.RefreshCollectSourceCatalog()
	if cur := CollectSvc.CurrentPrimary(); cur == nil || cur.Id != "src_p" {
		t.Fatalf("expected primary src_p, got %+v", cur)
	}

	h := &CollectHandler{}

	// 1. 普通管理员尝试停用当前主站并交接，必须被 403 拒绝
	saveBody, _ := json.Marshal(map[string]any{
		"id":           "src_p",
		"name":         "主站",
		"uri":          "http://127.0.0.1:59995/p",
		"state":        false,
		"newPrimaryId": "src_s",
	})
	c1, w1 := testContext(http.MethodPost, "/api/manage/collect/save")
	c1.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 20001, Role: model.UserRoleNormal})
	c1.Request = httptest.NewRequest(http.MethodPost, "/api/manage/collect/save", bytes.NewReader(saveBody))
	c1.Request.Header.Set("Content-Type", "application/json")
	h.FilmSourceSave(c1)
	if w1.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin disabling primary, got %d body=%s", w1.Code, w1.Body.String())
	}

	// 2. 普通管理员尝试删除当前主站并交接，必须被 403 拒绝
	delBody, _ := json.Marshal(map[string]any{
		"ids":          []string{"src_p"},
		"newPrimaryId": "src_s",
	})
	c2, w2 := testContext(http.MethodPost, "/api/manage/collect/delete")
	c2.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 20001, Role: model.UserRoleNormal})
	c2.Request = httptest.NewRequest(http.MethodPost, "/api/manage/collect/delete", bytes.NewReader(delBody))
	c2.Request.Header.Set("Content-Type", "application/json")
	h.FilmSourceDelBatch(c2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin deleting primary, got %d body=%s", w2.Code, w2.Body.String())
	}

	// 3. FilmSourceCheckAll 必须跳过当前主站，只检测其他站点
	c3, w3 := testContext(http.MethodPost, "/api/manage/collect/check/all")
	h.FilmSourceCheckAll(c3)
	resp3 := decodeResponse(t, w3)
	if resp3.Code != dto.SUCCESS {
		t.Fatalf("FilmSourceCheckAll failed: %+v", resp3)
	}
	rawData, _ := json.Marshal(resp3.Data)
	var checkRes struct {
		Skipped []model.SourceHealthItem `json:"skipped"`
		Failed  []model.SourceHealthItem `json:"failed"`
	}
	_ = json.Unmarshal(rawData, &checkRes)
	foundPrimarySkipped := false
	foundProxySkipped := false
	for _, item := range checkRes.Skipped {
		if item.Id == "src_p" {
			foundPrimarySkipped = true
		}
		if item.Id == "src_proxy" {
			foundProxySkipped = true
		}
	}
	if !foundPrimarySkipped {
		t.Fatalf("expected primary src_p in skipped, got skipped=%+v failed=%+v", checkRes.Skipped, checkRes.Failed)
	}
	if !foundProxySkipped {
		t.Fatalf("expected proxy source src_proxy in skipped when global spider proxy disabled, got skipped=%+v failed=%+v", checkRes.Skipped, checkRes.Failed)
	}
}

func TestCollectHandler_PrimaryStateReportsDeletingPrimary(t *testing.T) {
	gdb := setupCollectHandlerTestDB(t)
	if err := gdb.Create(&model.FilmSource{
		Id:    "src_del_p",
		Name:  "待删主站",
		Uri:   "http://127.0.0.1:59994/p",
		State: true,
		Sort:  0,
	}).Error; err != nil {
		t.Fatal(err)
	}
	_ = sourcecatalog.RefreshCollectSourceCatalog()
	if cur := CollectSvc.CurrentPrimary(); cur == nil || cur.Id != "src_del_p" {
		t.Fatalf("expected primary src_del_p, got %+v", cur)
	}

	h := &CollectHandler{}
	progress.SetSourceDeleting("src_del_p", true)
	defer progress.SetSourceDeleting("src_del_p", false)

	c1, w1 := testContext(http.MethodGet, "/api/manage/primary")
	h.PrimaryState(c1)
	resp1 := decodeResponse(t, w1)
	if resp1.Code != dto.SUCCESS {
		t.Fatalf("PrimaryState failed: %+v", resp1)
	}
	dataMap, ok := resp1.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data while deleting, got %T (%+v)", resp1.Data, resp1.Data)
	}
	ds, ok := dataMap["deletingSource"].(map[string]any)
	if !ok || ds["id"] != "src_del_p" || ds["name"] != "待删主站" {
		t.Fatalf("unexpected deletingSource: %+v", dataMap)
	}

	progress.SetSourceDeleting("src_del_p", false)
	c2, w2 := testContext(http.MethodGet, "/api/manage/primary")
	h.PrimaryState(c2)
	resp2 := decodeResponse(t, w2)
	dataMap2, ok := resp2.Data.(map[string]any)
	if !ok || dataMap2["id"] != "src_del_p" {
		t.Fatalf("expected restored primary after clearing deleting, got %+v", resp2.Data)
	}
}
