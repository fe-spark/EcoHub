package siteops

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"server/internal/config"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

func testResetContext(method, path string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, nil)
	return c, w
}

func decodeResetResponse(t *testing.T, w *httptest.ResponseRecorder) dto.Response {
	t.Helper()
	var resp dto.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

func TestSpiderHandler_ClearAllFilm_AuthAndValidation(t *testing.T) {
	// 1. Missing claims -> 401
	c, w := testResetContext(http.MethodPost, "/api/manage/spider/clear")
	ResetHd.ClearAllFilm(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	// 2. Non-admin -> 403
	c, w = testResetContext(http.MethodPost, "/api/manage/spider/clear")
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 20001, Role: model.UserRoleNormal})
	ResetHd.ClearAllFilm(c)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}

	// 3. Admin with invalid body -> Failed
	c, w = testResetContext(http.MethodPost, "/api/manage/spider/clear")
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: config.UserIdInitialVal, Role: model.UserRoleAdmin})
	c.Request.Body = http.NoBody
	ResetHd.ClearAllFilm(c)
	resp := decodeResetResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on invalid JSON, got %d", resp.Code)
	}

	// 4. Admin with wrong password -> Failed
	c, w = testResetContext(http.MethodPost, "/api/manage/spider/clear")
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: config.UserIdInitialVal, Role: model.UserRoleAdmin})
	body, _ := json.Marshal(map[string]string{"password": "wrong-password"})
	c.Request, _ = http.NewRequest(http.MethodPost, "/api/manage/spider/clear", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	ResetHd.ClearAllFilm(c)
	resp = decodeResetResponse(t, w)
	if resp.Code != dto.FAILED || resp.Msg != "重置失败, 密钥校验失败!!!" {
		t.Fatalf("expected password verification error, got code=%d msg=%q", resp.Code, resp.Msg)
	}
}
