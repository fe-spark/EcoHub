package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"server/internal/config"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/utils"
)

func TestSpiderHandler_ResetImpactStats(t *testing.T) {
	c, w := testContext(http.MethodGet, "/api/manage/spider/clear/stats")
	SpiderHd.ResetImpactStats(c)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected code SUCCESS, got %d", resp.Code)
	}
}

func TestSpiderHandler_ClearAllFilm_AuthAndValidation(t *testing.T) {
	// 1. Missing claims -> 401
	c, w := testContext(http.MethodPost, "/api/manage/spider/clear")
	SpiderHd.ClearAllFilm(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	// 2. Non-admin -> 403
	c, w = testContext(http.MethodPost, "/api/manage/spider/clear")
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 20001, Role: model.UserRoleNormal})
	SpiderHd.ClearAllFilm(c)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}

	// 3. Admin with invalid body -> Failed
	c, w = testContext(http.MethodPost, "/api/manage/spider/clear")
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: config.UserIdInitialVal, Role: model.UserRoleAdmin})
	c.Request.Body = http.NoBody
	SpiderHd.ClearAllFilm(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on invalid JSON, got %d", resp.Code)
	}

	// 4. Admin with wrong password -> Failed
	c, w = testContext(http.MethodPost, "/api/manage/spider/clear")
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: config.UserIdInitialVal, Role: model.UserRoleAdmin})
	body, _ := json.Marshal(map[string]string{"password": "wrong-password"})
	c.Request, _ = http.NewRequest(http.MethodPost, "/api/manage/spider/clear", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	SpiderHd.ClearAllFilm(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.FAILED || resp.Msg != "重置失败, 密钥校验失败!!!" {
		t.Fatalf("expected password verification error, got code=%d msg=%q", resp.Code, resp.Msg)
	}
}
