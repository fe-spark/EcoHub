package backup

import (
	"net/http"
	"server/internal/config"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/testutil"
	"server/internal/utils"
	"testing"
)

func TestExportConfigBackup_PasswordValidation(t *testing.T) {
	// 1. 无凭证 -> 401
	c, w := testutil.NewJSONContext(http.MethodPost, "/api/manage/config/backup/export", `{"password":"test"}`)
	BackupHd.ExportConfigBackup(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthenticated export, got %d", w.Code)
	}

	// 2. 非超管 -> 403
	c, w = testutil.NewJSONContext(http.MethodPost, "/api/manage/config/backup/export", `{"password":"test"}`)
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 20001, Role: model.UserRoleNormal})
	BackupHd.ExportConfigBackup(c)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on non-admin export, got %d", w.Code)
	}

	// 3. 超管但密码为空 -> FAILED
	c, w = testutil.NewJSONContext(http.MethodPost, "/api/manage/config/backup/export", `{"password":""}`)
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: config.UserIdInitialVal, Role: model.UserRoleAdmin})
	BackupHd.ExportConfigBackup(c)
	resp := testutil.DecodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty password, got code=%d", resp.Code)
	}
}
