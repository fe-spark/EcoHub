package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"server/internal/config"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/utils"
)

func TestSystemLogHandler_Delta(t *testing.T) {
	// 1. Missing claims -> 401
	c, w := testContext(http.MethodGet, "/api/manage/system/logs/delta?lines=10")
	SystemLogHd.Delta(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on missing claims, got %d", w.Code)
	}

	// 2. Non-admin claims -> 403
	c, w = testContext(http.MethodGet, "/api/manage/system/logs/delta?lines=10")
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 20002, Role: model.UserRoleNormal})
	SystemLogHd.Delta(c)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on normal user, got %d", w.Code)
	}

	// 3. Admin user initial fetch (lines=10)
	c, w = testContext(http.MethodGet, "/api/manage/system/logs/delta?lines=10")
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: config.UserIdInitialVal, Role: model.UserRoleAdmin})
	SystemLogHd.Delta(c)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected code SUCCESS, got %d", resp.Code)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", resp.Data)
	}
	if _, ok := data["entries"]; !ok {
		t.Errorf("missing entries in response data")
	}
	if _, ok := data["nextSeq"]; !ok {
		t.Errorf("missing nextSeq in response data")
	}

	// 4. Admin incremental fetch with after
	c2, w2 := testContext(http.MethodGet, "/api/manage/system/logs/delta?after=1&limit=50")
	c2.Set(config.AuthUserClaims, &utils.UserClaims{UserID: config.UserIdInitialVal, Role: model.UserRoleAdmin})
	SystemLogHd.Delta(c2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w2.Code)
	}
	resp2 := decodeResponse(t, w2)
	if resp2.Code != dto.SUCCESS {
		t.Fatalf("expected code SUCCESS, got %d", resp2.Code)
	}
}

func TestSystemLogHandler_StreamAuthAndSnapshot(t *testing.T) {
	c, w := testContext(http.MethodGet, "/api/manage/system/logs/stream")
	SystemLogHd.Stream(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on missing claims, got %d", w.Code)
	}

	c, w = testContext(http.MethodGet, "/api/manage/system/logs/stream")
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 20002, Role: model.UserRoleNormal})
	SystemLogHd.Stream(c)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on normal user, got %d", w.Code)
	}

	marker := "handler-stream-" + t.Name()
	syslog.Infof("%s", marker)
	c, w = testContext(http.MethodGet, "/api/manage/system/logs/stream?lines=50")
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: config.UserIdInitialVal, Role: model.UserRoleAdmin})
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	go func() {
		time.Sleep(time.Second)
		cancel()
	}()
	SystemLogHd.Stream(c)
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, body)
	}
	if !strings.Contains(body, marker) || !strings.Contains(body, `"reset":true`) {
		t.Fatalf("snapshot missing: %s", body)
	}
}
