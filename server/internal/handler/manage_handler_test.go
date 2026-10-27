package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"fmt"
	"strings"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupManageConfigDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.SiteConfigRecord{}, &model.Banner{}, &model.MappingRule{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })
}

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

func TestUpgradeAppRejectsNonAdmin(t *testing.T) {
	h := &ManageHandler{}
	cases := []struct {
		name   string
		claims any
		status int
		msg    string
	}{
		{name: "no claims", status: http.StatusUnauthorized, msg: "鉴权失败,请重新登录"},
		{name: "nil claims", claims: (*utils.UserClaims)(nil), status: http.StatusForbidden, msg: "权限不足，仅超级管理员可执行版本升级"},
		{
			name:   "normal user",
			claims: &utils.UserClaims{UserID: 10001, UserName: "user", Role: model.UserRoleNormal},
			status: http.StatusForbidden,
			msg:    "权限不足，仅超级管理员可执行版本升级",
		},
		{
			name:   "visitor",
			claims: &utils.UserClaims{UserID: 10002, UserName: "guest", Role: model.UserRoleVisitor},
			status: http.StatusForbidden,
			msg:    "权限不足，仅超级管理员可执行版本升级",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, w := testContext(http.MethodPost, "/api/manage/version/upgrade")
			if tc.claims != nil {
				c.Set(config.AuthUserClaims, tc.claims)
			}
			h.UpgradeApp(c)
			if w.Code != tc.status {
				t.Fatalf("status=%d want %d body=%s", w.Code, tc.status, w.Body.String())
			}
			resp := decodeResponse(t, w)
			if resp.Code != dto.FAILED {
				t.Fatalf("code=%d want %d", resp.Code, dto.FAILED)
			}
			if resp.Msg != tc.msg {
				t.Fatalf("msg=%q want %q", resp.Msg, tc.msg)
			}
		})
	}
}

func TestAppVersionSkipsUpdateCheckForNonAdmin(t *testing.T) {
	h := &ManageHandler{}
	c, w := testContext(http.MethodGet, "/api/manage/version")
	c.Set(config.AuthUserClaims, &utils.UserClaims{
		UserID:   10001,
		UserName: "user",
		Role:     model.UserRoleNormal,
	})
	h.AppVersion(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200 body=%s", w.Code, w.Body.String())
	}
	resp := decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("code=%d want 0", resp.Code)
	}
	raw, err := json.Marshal(resp.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var info service.AppVersionInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		t.Fatalf("decode version info: %v", err)
	}
	if info.HasUpdate {
		t.Fatalf("non-admin must not receive hasUpdate")
	}
	if info.CanUpgrade {
		t.Fatalf("non-admin must not receive canUpgrade")
	}
	if info.Latest != "" || info.ReleaseURL != "" || info.ReleaseNotes != "" {
		t.Fatalf("non-admin must not receive release payload: %+v", info)
	}
	if info.UpgradePhase != "" || info.UpgradeError != "" {
		t.Fatalf("non-admin must not receive upgrade state: phase=%q err=%q", info.UpgradePhase, info.UpgradeError)
	}
}

func TestVerifyManagePassword_SafeNoSideEffects(t *testing.T) {
	cases := []struct {
		name   string
		claims any
		set    bool
	}{
		{name: "missing claims", set: false},
		{name: "nil value", set: true, claims: nil},
		{name: "wrong type", set: true, claims: "invalid-type"},
		{name: "typed nil pointer", set: true, claims: (*utils.UserClaims)(nil)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, w := testContext(http.MethodPost, "/api/manage/spider/clear")
			if tc.set {
				c.Set(config.AuthUserClaims, tc.claims)
			}
			ok := verifyManagePassword(c, "some-password")
			if ok {
				t.Fatalf("expected false, got true")
			}
			if w.Body.Len() != 0 {
				t.Fatalf("expected no side effects on context recorder, got body=%s", w.Body.String())
			}
		})
	}
}

func TestUpdateSiteAccess_Validation(t *testing.T) {
	h := &ManageHandler{}

	t.Run("PrivateAccess true requires ProvideKey", func(t *testing.T) {
		c, w := testContext(http.MethodPost, "/api/manage/config/access")
		c.Request.Header.Set("Content-Type", "application/json")
		c.Request.Body = http.NoBody
		h.UpdateSiteAccess(c)
		resp := decodeResponse(t, w)
		if resp.Code != dto.FAILED {
			t.Fatalf("code=%d want %d", resp.Code, dto.FAILED)
		}
	})
}

func TestSiteAccessConfig_HidesProvideKeyForNonAdmin(t *testing.T) {
	setupManageConfigDB(t)
	h := &ManageHandler{}
	cases := []struct {
		name   string
		claims any
	}{
		{name: "no claims", claims: nil},
		{
			name:   "visitor",
			claims: &utils.UserClaims{UserID: 10002, UserName: "guest", Role: model.UserRoleVisitor},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, w := testContext(http.MethodGet, "/api/manage/config/access")
			if tc.claims != nil {
				c.Set(config.AuthUserClaims, tc.claims)
			}
			h.SiteAccessConfig(c)
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d want 200", w.Code)
			}
			resp := decodeResponse(t, w)
			if resp.Code != dto.SUCCESS {
				t.Fatalf("code=%d want %d", resp.Code, dto.SUCCESS)
			}
			raw, _ := json.Marshal(resp.Data)
			var cfg model.AccessConfig
			if err := json.Unmarshal(raw, &cfg); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if cfg.ProvideKey != "" {
				t.Fatalf("expected empty provideKey for non-admin, got %q", cfg.ProvideKey)
			}
		})
	}
}

func TestBannerHandlers(t *testing.T) {
	setupManageConfigDB(t)
	h := &ManageHandler{}

	c, w := testContext(http.MethodGet, "/api/manage/banner/list")
	h.BannerList(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200 body=%s", w.Code, w.Body.String())
	}
	resp := decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("code=%d want 0", resp.Code)
	}

	// 测试一键排片参数解析异常与成功处理
	cGen, wGen := testJSONContext(http.MethodPost, "/api/manage/banner/quick-generate", `{"count":6,"strategy":"hot"}`)
	h.BannerQuickGenerate(cGen)
	if wGen.Code != http.StatusOK {
		t.Fatalf("status=%d want 200 body=%s", wGen.Code, wGen.Body.String())
	}
}

func TestBannerAndMappingRule_SaveAndDelete_BatchContract(t *testing.T) {
	setupManageConfigDB(t)
	h := &ManageHandler{}

	// 1. BannerSave (新增，不传 ID)
	bannerAddBody := `{"title":"测试轮播","mid":101,"poster":"http://test.jpg"}`
	c, w := testJSONContext(http.MethodPost, "/api/manage/banner/save", bannerAddBody)
	h.BannerSave(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected BannerSave success, got code=%d msg=%s", resp.Code, resp.Msg)
	}

	// 获取生成的 banner
	banners, err := service.ManageSvc.GetBanners()
	if err != nil || len(banners) != 1 {
		t.Fatalf("expected 1 banner, got %v, err=%v", len(banners), err)
	}
	bannerID := banners[0].Id

	// 2. BannerDelete 批量删除
	delBody, _ := json.Marshal(map[string][]string{"ids": {bannerID}})
	c, w = testJSONContext(http.MethodPost, "/api/manage/banner/delete", string(delBody))
	h.BannerDelete(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected BannerDelete success, got code=%d msg=%s", resp.Code, resp.Msg)
	}

	bannersAfter, _ := service.ManageSvc.GetBanners()
	if len(bannersAfter) != 0 {
		t.Fatalf("expected 0 banners after delete, got %d", len(bannersAfter))
	}

	// 3. MappingRuleSave (新增，ID=0)
	ruleAddBody := `{"raw":"大陆","target":"中国大陆","matchType":"exact","group":"Area"}`
	c, w = testJSONContext(http.MethodPost, "/api/manage/mapping/rule/save", ruleAddBody)
	h.MappingRuleSave(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected MappingRuleSave success, got code=%d msg=%s", resp.Code, resp.Msg)
	}

	// 4. MappingRuleDelete 批量删除
	delRuleBody, _ := json.Marshal(map[string][]uint{"ids": {1}})
	c, w = testJSONContext(http.MethodPost, "/api/manage/mapping/rule/delete", string(delRuleBody))
	h.MappingRuleDelete(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected MappingRuleDelete success, got code=%d msg=%s", resp.Code, resp.Msg)
	}
}

func TestUserHandler_SaveAndDelete_Validation(t *testing.T) {
	// 1. 无凭证调用 UserSave -> Failed
	c, w := testJSONContext(http.MethodPost, "/api/manage/user/save", `{"userName":"test"}`)
	UserHd.UserSave(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on unauthenticated UserSave, got %d", resp.Code)
	}

	// 2. 无凭证调用 UserDelete -> Failed
	c, w = testJSONContext(http.MethodPost, "/api/manage/user/delete", `{"ids":[100]}`)
	UserHd.UserDelete(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on unauthenticated UserDelete, got %d", resp.Code)
	}
}

func TestCronHandler_StateAndRun_Validation(t *testing.T) {
	// 1. 空 ids 变更状态 -> Failed
	c, w := testJSONContext(http.MethodPost, "/api/manage/cron/state", `{"ids":[],"state":true}`)
	CronHd.ChangeTaskState(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty ids for ChangeTaskState, got %d", resp.Code)
	}

	// 2. 空 ids 执行任务 -> Failed
	c, w = testJSONContext(http.MethodPost, "/api/manage/cron/run", `{"ids":[]}`)
	CronHd.RunFilmCronTask(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty ids for RunFilmCronTask, got %d", resp.Code)
	}
}

func TestExportConfigBackup_PasswordValidation(t *testing.T) {
	// 1. 无凭证 -> 401
	c, w := testJSONContext(http.MethodPost, "/api/manage/config/backup/export", `{"password":"test"}`)
	ManageHd.ExportConfigBackup(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthenticated export, got %d", w.Code)
	}

	// 2. 非超管 -> 403
	c, w = testJSONContext(http.MethodPost, "/api/manage/config/backup/export", `{"password":"test"}`)
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 20001, Role: model.UserRoleNormal})
	ManageHd.ExportConfigBackup(c)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on non-admin export, got %d", w.Code)
	}

	// 3. 超管但密码为空 -> FAILED
	c, w = testJSONContext(http.MethodPost, "/api/manage/config/backup/export", `{"password":""}`)
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: config.UserIdInitialVal, Role: model.UserRoleAdmin})
	ManageHd.ExportConfigBackup(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty password, got code=%d", resp.Code)
	}
}
