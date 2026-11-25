package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"server/internal/config"
	"server/internal/httpx"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/testutil"
	"server/internal/user"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupManageConfigDB(t *testing.T) {
	t.Helper()
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.SiteConfigRecord{}, &model.BannerSlot{}, &model.FilmIndex{}, &model.FilmSourcePlaylist{}, &model.FilmSource{}, &model.MappingRule{}, &model.TMDBConfigRecord{}, &model.User{}, &model.FileInfo{}, &model.PrimaryState{}); err != nil {
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
	var info AppVersionInfo
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
			ok := httpx.VerifyCurrentPassword(c, "some-password", user.UserSvc.VerifyUserPassword)
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

func TestUserHandler_SaveAndDelete_Validation(t *testing.T) {
	// 1. 无凭证调用 UserSave -> Failed
	c, w := testJSONContext(http.MethodPost, "/api/manage/user/save", `{"userName":"test"}`)
	user.UserHd.UserSave(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on unauthenticated UserSave, got %d", resp.Code)
	}

	// 2. 无凭证调用 UserDelete -> Failed
	c, w = testJSONContext(http.MethodPost, "/api/manage/user/delete", `{"ids":[100]}`)
	user.UserHd.UserDelete(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on unauthenticated UserDelete, got %d", resp.Code)
	}

	// 3. 超管使用 string id 更新用户 -> 成功
	setupManageConfigDB(t)
	salt, _ := utils.GenerateSalt()
	u10005 := model.User{
		Model:    gorm.Model{ID: 10005},
		UserName: "testuser10005",
		NickName: "OldNick",
		Role:     model.UserRoleNormal,
		Status:   0,
		Password: utils.PasswordEncrypt("pwd123", salt),
		Salt:     salt,
	}
	db.Mdb.Create(&u10005)

	c, w = testContext(http.MethodPost, "/api/manage/user/save")
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: config.UserIdInitialVal, UserName: "admin", Role: model.UserRoleAdmin})
	c.Request = httptest.NewRequest(http.MethodPost, "/api/manage/user/save", bytes.NewReader([]byte(`{"id":"10005","nickName":"StringNick"}`)))
	c.Request.Header.Set("Content-Type", "application/json")
	user.UserHd.UserSave(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected SUCCESS on string id UserSave, got code=%d msg=%s", resp.Code, resp.Msg)
	}
	var checkU model.User
	db.Mdb.First(&checkU, 10005)
	if checkU.NickName != "StringNick" {
		t.Fatalf("expected NickName StringNick, got %s", checkU.NickName)
	}

	// 4. 超管使用大写 ID (number) 更新用户 -> 成功
	c, w = testContext(http.MethodPost, "/api/manage/user/save")
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: config.UserIdInitialVal, UserName: "admin", Role: model.UserRoleAdmin})
	c.Request = httptest.NewRequest(http.MethodPost, "/api/manage/user/save", bytes.NewReader([]byte(`{"ID":10005,"nickName":"CapIDNick"}`)))
	c.Request.Header.Set("Content-Type", "application/json")
	user.UserHd.UserSave(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected SUCCESS on uppercase ID UserSave, got code=%d msg=%s", resp.Code, resp.Msg)
	}
	db.Mdb.First(&checkU, 10005)
	if checkU.NickName != "CapIDNick" {
		t.Fatalf("expected NickName CapIDNick, got %s", checkU.NickName)
	}
}
