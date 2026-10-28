package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/utils"

	"fmt"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestRouterPermissions_SpiderClearAndSystemLogs(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis run: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	prevRdb := db.Rdb
	db.Rdb = rdb
	defer func() { db.Rdb = prevRdb }()

	dsn := fmt.Sprintf("file:router_%d?mode=memory&cache=shared&_busy_timeout=5000", time.Now().UnixNano())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	_ = gdb.AutoMigrate(&model.User{})
	prevMdb := db.Mdb
	db.Mdb = gdb
	defer func() { db.Mdb = prevMdb }()

	adminUserID := uint(config.UserIdInitialVal)
	adminToken, err := utils.GenToken(adminUserID, "admin", model.UserRoleAdmin)
	if err != nil {
		t.Fatalf("gen admin token: %v", err)
	}
	if err := repository.SaveUserToken(adminToken, adminUserID); err != nil {
		t.Fatalf("save admin token: %v", err)
	}

	normalUserID := uint(20002)
	normalToken, err := utils.GenToken(normalUserID, "editor", model.UserRoleNormal)
	if err != nil {
		t.Fatalf("gen normal token: %v", err)
	}
	if err := repository.SaveUserToken(normalToken, normalUserID); err != nil {
		t.Fatalf("save normal token: %v", err)
	}

	gdb.Create(&model.User{Model: gorm.Model{ID: adminUserID}, Role: model.UserRoleAdmin, Status: 0})
	gdb.Create(&model.User{Model: gorm.Model{ID: normalUserID}, Role: model.UserRoleNormal, Status: 0})

	r := SetupRouter()

	performReq := func(method, path, token string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, nil)
		if token != "" {
			req.AddCookie(&http.Cookie{
				Name:  config.AuthCookieName,
				Value: token,
			})
		}
		r.ServeHTTP(w, req)
		return w
	}

	// 1. /api/manage/spider/clear/stats allows any authenticated manage user (non-admin)
	wNormalStats := performReq(http.MethodGet, "/api/manage/spider/clear/stats", normalToken)
	if wNormalStats.Code == http.StatusForbidden || wNormalStats.Code == http.StatusUnauthorized {
		t.Errorf("GET /api/manage/spider/clear/stats should allow normal user, got %d", wNormalStats.Code)
	}

	// 2. Destructive /api/manage/spider/clear MUST forbid non-admin
	wNormalClear := performReq(http.MethodPost, "/api/manage/spider/clear", normalToken)
	if wNormalClear.Code != http.StatusForbidden {
		t.Errorf("POST /api/manage/spider/clear should forbid normal user, got %d", wNormalClear.Code)
	}

	// 3. Reset progress stream is admin-only
	wNormalProg := performReq(http.MethodGet, "/api/manage/spider/clear/progress", normalToken)
	if wNormalProg.Code != http.StatusForbidden {
		t.Errorf("GET /api/manage/spider/clear/progress should forbid non-admin, got %d", wNormalProg.Code)
	}
	wAdminProg := performReq(http.MethodGet, "/api/manage/spider/clear/progress", adminToken)
	if wAdminProg.Code == http.StatusForbidden {
		t.Errorf("GET /api/manage/spider/clear/progress should allow admin, got %d", wAdminProg.Code)
	}

	// 4. Admin user should be allowed through to the stats endpoint
	wAdminClearStats := performReq(http.MethodGet, "/api/manage/spider/clear/stats", adminToken)
	if wAdminClearStats.Code == http.StatusForbidden {
		t.Errorf("GET /api/manage/spider/clear/stats should allow admin, got %d", wAdminClearStats.Code)
	}

	// 5. /api/manage/access/status allows any authenticated manage user (non-admin)
	wNormalAccessStatus := performReq(http.MethodGet, "/api/manage/access/status", normalToken)
	if wNormalAccessStatus.Code == http.StatusForbidden || wNormalAccessStatus.Code == http.StatusUnauthorized {
		t.Errorf("GET /api/manage/access/status should allow normal manage user, got %d", wNormalAccessStatus.Code)
	}

	// 5. Access stats and clean MUST forbid non-admin
	wNormalAccessStats := performReq(http.MethodGet, "/api/manage/access/stats", normalToken)
	if wNormalAccessStats.Code != http.StatusForbidden {
		t.Errorf("GET /api/manage/access/stats should forbid normal user, got %d", wNormalAccessStats.Code)
	}

	wNormalAccessClean := performReq(http.MethodPost, "/api/manage/access/clean", normalToken)
	if wNormalAccessClean.Code != http.StatusForbidden {
		t.Errorf("POST /api/manage/access/clean should forbid normal user, got %d", wNormalAccessClean.Code)
	}

	wNormalAccessOverview := performReq(http.MethodGet, "/api/manage/access/overview", normalToken)
	if wNormalAccessOverview.Code != http.StatusForbidden {
		t.Errorf("GET /api/manage/access/overview should forbid normal user, got %d", wNormalAccessOverview.Code)
	}

	// 6. Admin user should be allowed through to access admin endpoints
	wAdminAccessStatus := performReq(http.MethodGet, "/api/manage/access/status", adminToken)
	if wAdminAccessStatus.Code == http.StatusForbidden {
		t.Errorf("GET /api/manage/access/status should allow admin, got %d", wAdminAccessStatus.Code)
	}

	wAdminAccessStats := performReq(http.MethodGet, "/api/manage/access/stats", adminToken)
	if wAdminAccessStats.Code == http.StatusForbidden {
		t.Errorf("GET /api/manage/access/stats should allow admin, got %d", wAdminAccessStats.Code)
	}

	// 7. System settings routes: system logs MUST forbid non-admin
	wNormalLogsDelta := performReq(http.MethodGet, "/api/manage/system/logs/delta", normalToken)
	if wNormalLogsDelta.Code != http.StatusForbidden {
		t.Errorf("GET /api/manage/system/logs/delta should forbid normal user, got %d", wNormalLogsDelta.Code)
	}
	wNormalLogsStream := performReq(http.MethodGet, "/api/manage/system/logs/stream", normalToken)
	if wNormalLogsStream.Code != http.StatusForbidden {
		t.Errorf("GET /api/manage/system/logs/stream should forbid normal user, got %d", wNormalLogsStream.Code)
	}

	// 8. Admin user allowed through to system logs
	wAdminLogsDelta := performReq(http.MethodGet, "/api/manage/system/logs/delta", adminToken)
	if wAdminLogsDelta.Code == http.StatusForbidden {
		t.Errorf("GET /api/manage/system/logs/delta should allow admin, got %d", wAdminLogsDelta.Code)
	}

	// 9. Notify routes: GET config allows normal manage user, POST update/test forbids non-admin
	wNormalNotifyGet := performReq(http.MethodGet, "/api/manage/notify/config", normalToken)
	if wNormalNotifyGet.Code == http.StatusForbidden || wNormalNotifyGet.Code == http.StatusUnauthorized {
		t.Errorf("GET /api/manage/notify/config should allow normal manage user, got %d", wNormalNotifyGet.Code)
	}
	wNormalNotifyUpdate := performReq(http.MethodPost, "/api/manage/notify/config", normalToken)
	if wNormalNotifyUpdate.Code != http.StatusForbidden {
		t.Errorf("POST /api/manage/notify/config should forbid normal user, got %d", wNormalNotifyUpdate.Code)
	}
	wNormalNotifyTest := performReq(http.MethodPost, "/api/manage/notify/config/test", normalToken)
	if wNormalNotifyTest.Code != http.StatusForbidden {
		t.Errorf("POST /api/manage/notify/config/test should forbid normal user, got %d", wNormalNotifyTest.Code)
	}

	// 10. Additional hardened routes: basic config, film delete, collect save, tmdb scrape MUST forbid non-admin
	wNormalBasicConfig := performReq(http.MethodPost, "/api/manage/config/basic", normalToken)
	if wNormalBasicConfig.Code != http.StatusForbidden {
		t.Errorf("POST /api/manage/config/basic should forbid normal user, got %d", wNormalBasicConfig.Code)
	}
	wNormalFilmDelete := performReq(http.MethodPost, "/api/manage/film/delete", normalToken)
	if wNormalFilmDelete.Code != http.StatusForbidden {
		t.Errorf("POST /api/manage/film/delete should forbid normal user, got %d", wNormalFilmDelete.Code)
	}
	wNormalCollectSave := performReq(http.MethodPost, "/api/manage/collect/save", normalToken)
	if wNormalCollectSave.Code != http.StatusForbidden {
		t.Errorf("POST /api/manage/collect/save should forbid normal user, got %d", wNormalCollectSave.Code)
	}
	wNormalTMDBScrape := performReq(http.MethodPost, "/api/manage/tmdb/scrape", normalToken)
	if wNormalTMDBScrape.Code != http.StatusForbidden {
		t.Errorf("POST /api/manage/tmdb/scrape should forbid normal user, got %d", wNormalTMDBScrape.Code)
	}

	// 11. Profile endpoint allows normal authenticated user (not 403)
	wNormalUserProfile := performReq(http.MethodPost, "/api/manage/user/profile", normalToken)
	if wNormalUserProfile.Code == http.StatusForbidden || wNormalUserProfile.Code == http.StatusUnauthorized {
		t.Errorf("POST /api/manage/user/profile should allow normal user, got %d", wNormalUserProfile.Code)
	}
}
