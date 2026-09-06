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

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTrackViewMaxBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	raw := `{"action":"browse","resource":"` + strings.Repeat("a", 8000) + `"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/stat/view", strings.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	AccessHd.TrackView(c)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("want ok envelope, got %+v", resp)
	}
}

func TestTrackViewValidPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	raw := `{"action":"browse","path":"/filmClassify","page_title":"分类","device_id":"eh_did_12345"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/stat/view", strings.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	AccessHd.TrackView(c)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("want code 0, got %+v", resp)
	}
}

func TestAccessOverviewAndTopsHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	orig := config.AccessLogEnabled
	defer func() { config.AccessLogEnabled = orig }()

	// 1. 关闭状态下测试
	config.AccessLogEnabled = false
	{
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manage/access/status", nil)
		AccessHd.Status(c)
		resp := decodeResponse(t, w)
		if resp.Code != 0 {
			t.Fatalf("want code 0, got %+v", resp)
		}

		w2 := httptest.NewRecorder()
		c2, _ := gin.CreateTestContext(w2)
		c2.Request = httptest.NewRequest(http.MethodGet, "/manage/access/overview?day=2020-01-01&module=web", nil)
		AccessHd.Overview(c2)
		resp2 := decodeResponse(t, w2)
		if resp2.Code == 0 {
			t.Fatalf("overview should fail when disabled, got %+v", resp2)
		}

		w3 := httptest.NewRecorder()
		c3, _ := gin.CreateTestContext(w3)
		c3.Request = httptest.NewRequest(http.MethodGet, "/manage/access/tops?day=2020-01-01&kind=play&module=web&limit=5", nil)
		AccessHd.Tops(c3)
		resp3 := decodeResponse(t, w3)
		if resp3.Code == 0 {
			t.Fatalf("tops should fail when disabled, got %+v", resp3)
		}

		w4 := httptest.NewRecorder()
		c4, _ := gin.CreateTestContext(w4)
		c4.Request = httptest.NewRequest(http.MethodGet, "/manage/access/logs?day=2020-01-01&module=web&limit=10", nil)
		AccessHd.Logs(c4)
		resp4 := decodeResponse(t, w4)
		if resp4.Code == 0 {
			t.Fatalf("logs should fail when disabled, got %+v", resp4)
		}
	}

	// 2. 开启状态下测试
	config.AccessLogEnabled = true
	{
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/manage/access/status", nil)
		AccessHd.Status(c)
		resp := decodeResponse(t, w)
		if resp.Code != 0 {
			t.Fatalf("want code 0, got %+v", resp)
		}

		// 测试历史日期（无 Redis 时返回安全默认数据）
		w2 := httptest.NewRecorder()
		c2, _ := gin.CreateTestContext(w2)
		c2.Request = httptest.NewRequest(http.MethodGet, "/manage/access/overview?day=2020-01-01&module=web", nil)
		AccessHd.Overview(c2)
		if w2.Code != http.StatusOK {
			t.Fatalf("overview code=%d body=%s", w2.Code, w2.Body.String())
		}
		resp2 := decodeResponse(t, w2)
		if resp2.Code != 0 {
			t.Fatalf("want code 0, got %+v", resp2)
		}

		// 测试 Tops 榜单接口
		w3 := httptest.NewRecorder()
		c3, _ := gin.CreateTestContext(w3)
		c3.Request = httptest.NewRequest(http.MethodGet, "/manage/access/tops?day=2020-01-01&kind=play&module=web&limit=5", nil)
		AccessHd.Tops(c3)
		if w3.Code != http.StatusOK {
			t.Fatalf("tops code=%d body=%s", w3.Code, w3.Body.String())
		}
		resp3 := decodeResponse(t, w3)
		if resp3.Code != 0 {
			t.Fatalf("want code 0, got %+v", resp3)
		}

		// 测试 Logs 流水接口（当 Redis 不可用时安全返回错误响应而非 panic）
		w4 := httptest.NewRecorder()
		c4, _ := gin.CreateTestContext(w4)
		c4.Request = httptest.NewRequest(http.MethodGet, "/manage/access/logs?day=2020-01-01&module=web&limit=10", nil)
		AccessHd.Logs(c4)
		if w4.Code != http.StatusOK {
			t.Fatalf("logs code=%d body=%s", w4.Code, w4.Body.String())
		}
	}
}

func setupAccessDailyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.AccessDailyStats{}, &model.AccessDailyTop{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })
	return gdb
}

func TestAccessDisabledWithHistoricalData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gdb := setupAccessDailyTestDB(t)

	orig := config.AccessLogEnabled
	defer func() { config.AccessLogEnabled = orig }()
	config.AccessLogEnabled = false

	// 插入一条历史落库数据
	testDay := "2026-08-20"
	stat := model.AccessDailyStats{
		Day:        testDay,
		PV:         100,
		UV:         20,
		ClientJSON: `{"web":100}`,
		ActionJSON: `{"play":10}`,
		HistJSON:   `{}`,
		RolledAt:   time.Now(),
	}
	if err := gdb.Create(&stat).Error; err != nil {
		t.Fatalf("insert stat: %v", err)
	}

	// 1. 测试 Status 接口：enabled 为 false，但 hasData 为 true
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/manage/access/status", nil)
	AccessHd.Status(c)
	resp := decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("want code 0, got %+v", resp)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("want map data, got %+v", resp.Data)
	}
	if data["enabled"] != false {
		t.Fatalf("expected enabled=false, got %v", data["enabled"])
	}
	if data["hasData"] != true {
		t.Fatalf("expected hasData=true, got %v", data["hasData"])
	}

	// 2. 测试 Overview 接口：开关关闭但在有历史数据时必须允许查看
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/manage/access/overview?day="+testDay+"&module=web", nil)
	AccessHd.Overview(c2)
	if w2.Code != http.StatusOK {
		t.Fatalf("overview code=%d body=%s", w2.Code, w2.Body.String())
	}
	resp2 := decodeResponse(t, w2)
	if resp2.Code != 0 {
		t.Fatalf("overview should succeed when hasData is true, got %+v", resp2)
	}

	// 3. 测试 Tops 接口：开关关闭但在有历史数据时必须允许查看
	w3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(w3)
	c3.Request = httptest.NewRequest(http.MethodGet, "/manage/access/tops?day="+testDay+"&kind=play&module=web&limit=5", nil)
	AccessHd.Tops(c3)
	if w3.Code != http.StatusOK {
		t.Fatalf("tops code=%d body=%s", w3.Code, w3.Body.String())
	}
	resp3 := decodeResponse(t, w3)
	if resp3.Code != 0 {
		t.Fatalf("tops should succeed when hasData is true, got %+v", resp3)
	}

	// 4. 测试 Logs 接口：开关关闭但在有历史数据时允许调用
	w4 := httptest.NewRecorder()
	c4, _ := gin.CreateTestContext(w4)
	c4.Request = httptest.NewRequest(http.MethodGet, "/manage/access/logs?day="+testDay+"&module=web&limit=10", nil)
	AccessHd.Logs(c4)
	if w4.Code != http.StatusOK {
		t.Fatalf("logs code=%d body=%s", w4.Code, w4.Body.String())
	}
	resp4 := decodeResponse(t, w4)
	if resp4.Code != 0 {
		t.Fatalf("logs should succeed when hasData is true, got %+v", resp4)
	}
}

func TestDataStats_Auth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 1. Missing claims -> 401
	c, w := testContext(http.MethodGet, "/api/manage/access/stats")
	AccessHd.DataStats(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	// 2. Non-admin -> 403
	c, w = testContext(http.MethodGet, "/api/manage/access/stats")
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 20001, Role: model.UserRoleNormal})
	AccessHd.DataStats(c)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}

	// 3. Super admin -> 200 SUCCESS
	c, w = testContext(http.MethodGet, "/api/manage/access/stats")
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: config.UserIdInitialVal, Role: model.UserRoleAdmin})
	AccessHd.DataStats(c)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected SUCCESS, got %d", resp.Code)
	}
}

func TestCleanData_AuthAndValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 1. Missing claims -> 401
	c, w := testContext(http.MethodPost, "/api/manage/access/clean")
	AccessHd.CleanData(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	// 2. Non-admin -> 403
	c, w = testContext(http.MethodPost, "/api/manage/access/clean")
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 20001, Role: model.UserRoleNormal})
	AccessHd.CleanData(c)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}

	// 3. Admin with invalid body -> FAILED
	c, w = testContext(http.MethodPost, "/api/manage/access/clean")
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: config.UserIdInitialVal, Role: model.UserRoleAdmin})
	c.Request.Body = http.NoBody
	AccessHd.CleanData(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on invalid body, got %d", resp.Code)
	}

	// 4. Admin with wrong password -> FAILED
	c, w = testContext(http.MethodPost, "/api/manage/access/clean")
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: config.UserIdInitialVal, Role: model.UserRoleAdmin})
	body, _ := json.Marshal(map[string]any{"password": "wrong-password", "retentionDays": 7})
	c.Request, _ = http.NewRequest(http.MethodPost, "/api/manage/access/clean", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	AccessHd.CleanData(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.FAILED || resp.Msg != "清理失败, 密钥校验失败!!!" {
		t.Fatalf("expected password failed, got code=%d msg=%q", resp.Code, resp.Msg)
	}

	// 5. Admin with negative retention days -> FAILED
	c, w = testContext(http.MethodPost, "/api/manage/access/clean")
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: config.UserIdInitialVal, Role: model.UserRoleAdmin})
	body, _ = json.Marshal(map[string]any{"password": "valid-password", "retentionDays": -1})
	c.Request, _ = http.NewRequest(http.MethodPost, "/api/manage/access/clean", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	AccessHd.CleanData(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.FAILED || resp.Msg != "保留天数参数异常" {
		t.Fatalf("expected negative retention error, got code=%d msg=%q", resp.Code, resp.Msg)
	}

	// 6. Admin with empty password -> FAILED
	c, w = testContext(http.MethodPost, "/api/manage/access/clean")
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: config.UserIdInitialVal, Role: model.UserRoleAdmin})
	body, _ = json.Marshal(map[string]any{"password": "   ", "retentionDays": 0})
	c.Request, _ = http.NewRequest(http.MethodPost, "/api/manage/access/clean", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	AccessHd.CleanData(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.FAILED || resp.Msg != "清理失败, 密钥校验失败!!!" {
		t.Fatalf("expected empty password failure, got code=%d msg=%q", resp.Code, resp.Msg)
	}
}

