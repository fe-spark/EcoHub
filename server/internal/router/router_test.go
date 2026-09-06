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

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
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

	// 3. /api/manage/spider/clear/progress MUST forbid non-admin
	wNormalProg := performReq(http.MethodGet, "/api/manage/spider/clear/progress", normalToken)
	if wNormalProg.Code != http.StatusForbidden {
		t.Errorf("GET /api/manage/spider/clear/progress should forbid normal user, got %d", wNormalProg.Code)
	}

	// 4. Admin user should be allowed through to all admin endpoints
	wAdminClearStats := performReq(http.MethodGet, "/api/manage/spider/clear/stats", adminToken)
	if wAdminClearStats.Code == http.StatusForbidden {
		t.Errorf("GET /api/manage/spider/clear/stats should allow admin, got %d", wAdminClearStats.Code)
	}

	wAdminProgress := performReq(http.MethodGet, "/api/manage/spider/clear/progress", adminToken)
	if wAdminProgress.Code == http.StatusForbidden {
		t.Errorf("GET /api/manage/spider/clear/progress should allow admin, got %d", wAdminProgress.Code)
	}

	// 5. /api/manage/access/status allows any authenticated manage user (non-admin)
	wNormalAccessStatus := performReq(http.MethodGet, "/api/manage/access/status", normalToken)
	if wNormalAccessStatus.Code == http.StatusForbidden || wNormalAccessStatus.Code == http.StatusUnauthorized {
		t.Errorf("GET /api/manage/access/status should allow normal manage user, got %d", wNormalAccessStatus.Code)
	}

	// 6. Access stats and clean MUST forbid non-admin
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

	// 7. Admin user should be allowed through to access admin endpoints
	wAdminAccessStatus := performReq(http.MethodGet, "/api/manage/access/status", adminToken)
	if wAdminAccessStatus.Code == http.StatusForbidden {
		t.Errorf("GET /api/manage/access/status should allow admin, got %d", wAdminAccessStatus.Code)
	}

	wAdminAccessStats := performReq(http.MethodGet, "/api/manage/access/stats", adminToken)
	if wAdminAccessStats.Code == http.StatusForbidden {
		t.Errorf("GET /api/manage/access/stats should allow admin, got %d", wAdminAccessStats.Code)
	}
}

