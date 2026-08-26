package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"server/internal/config"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
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
