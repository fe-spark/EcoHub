package backup

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

func init() { gin.SetMode(gin.TestMode) }

func testJSONContext(method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, w
}

func decodeResponse(t *testing.T, w *httptest.ResponseRecorder) dto.Response {
	t.Helper()
	var resp dto.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	return resp
}

func TestExportConfigBackup_PasswordValidation(t *testing.T) {
	// 1. 无凭证 -> 401
	c, w := testJSONContext(http.MethodPost, "/api/manage/config/backup/export", `{"password":"test"}`)
	BackupHd.ExportConfigBackup(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthenticated export, got %d", w.Code)
	}

	// 2. 非超管 -> 403
	c, w = testJSONContext(http.MethodPost, "/api/manage/config/backup/export", `{"password":"test"}`)
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 20001, Role: model.UserRoleNormal})
	BackupHd.ExportConfigBackup(c)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on non-admin export, got %d", w.Code)
	}

	// 3. 超管但密码为空 -> FAILED
	c, w = testJSONContext(http.MethodPost, "/api/manage/config/backup/export", `{"password":""}`)
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: config.UserIdInitialVal, Role: model.UserRoleAdmin})
	BackupHd.ExportConfigBackup(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty password, got code=%d", resp.Code)
	}
}
