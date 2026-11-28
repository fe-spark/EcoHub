package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"server/internal/analytics/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/testutil"
	"server/internal/utils"
	appcfg "server/internal/config"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupHandlerEnv(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.AnalyticsConfigRecord{}, &model.AccessDailyStats{}, &model.AccessDailyTop{}); err != nil {
		t.Fatal(err)
	}

	prevRdb, prevMdb := db.Rdb, db.Mdb
	db.Rdb, db.Mdb = rdb, gdb
	t.Cleanup(func() { db.Rdb, db.Mdb = prevRdb, prevMdb })
}

func TestConfigAndAuthHandlers(t *testing.T) {
	setupHandlerEnv(t)
	prevEnabled := config.IsEnabled()
	t.Cleanup(func() { config.SetEnabled(prevEnabled) })

	// 1. UpdateConfig & GetConfig
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(map[string]any{"enabled": true})
	c.Request = httptest.NewRequest(http.MethodPost, "/api/manage/analytics/config", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	Handler.UpdateConfig(c)
	if w.Code != http.StatusOK || !config.IsEnabled() {
		t.Fatalf("expected UpdateConfig enabled=true, code=%d enabled=%v", w.Code, config.IsEnabled())
	}

	// 2. TrackView
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodPost, "/api/stats/view", strings.NewReader(`{"action":"browse","path":"/"}`))
	c2.Request.Header.Set("Content-Type", "application/json")
	Handler.TrackView(c2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected TrackView 200, got %d", w2.Code)
	}

	// 3. DataStats forbids non-admin
	w3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(w3)
	c3.Request = httptest.NewRequest(http.MethodGet, "/api/manage/analytics/stats", nil)
	c3.Set(appcfg.AuthUserClaims, &utils.UserClaims{UserID: 20001, Role: model.UserRoleNormal})
	Handler.DataStats(c3)
	if w3.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin DataStats, got %d", w3.Code)
	}

	// 4. DataStats allows super admin
	w4 := httptest.NewRecorder()
	c4, _ := gin.CreateTestContext(w4)
	c4.Request = httptest.NewRequest(http.MethodGet, "/api/manage/analytics/stats", nil)
	c4.Set(appcfg.AuthUserClaims, &utils.UserClaims{UserID: appcfg.UserIdInitialVal, Role: model.UserRoleAdmin})
	Handler.DataStats(c4)
	var resp dto.Response
	_ = json.Unmarshal(w4.Body.Bytes(), &resp)
	if w4.Code != http.StatusOK || resp.Code != dto.SUCCESS {
		t.Fatalf("expected 200 SUCCESS for admin DataStats, got http=%d code=%d", w4.Code, resp.Code)
	}
}
