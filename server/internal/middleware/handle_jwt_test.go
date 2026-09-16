package middleware

import (
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

func init() {
	gin.SetMode(gin.TestMode)
}

func TestAdminAccess(t *testing.T) {
	cases := []struct {
		name     string
		claims   any
		set      bool
		status   int
		wantNext bool
	}{
		{name: "no claims", status: http.StatusUnauthorized},
		{name: "nil claims", set: true, claims: (*utils.UserClaims)(nil), status: http.StatusUnauthorized},
		{
			name:   "normal user",
			set:    true,
			claims: &utils.UserClaims{UserID: 10001, Role: model.UserRoleNormal},
			status: http.StatusForbidden,
		},
		{
			name:   "visitor",
			set:    true,
			claims: &utils.UserClaims{UserID: 10002, Role: model.UserRoleVisitor},
			status: http.StatusForbidden,
		},
		{
			name:     "admin role",
			set:      true,
			claims:   &utils.UserClaims{UserID: 10001, Role: model.UserRoleAdmin},
			status:   http.StatusOK,
			wantNext: true,
		},
		{
			name:     "builtin admin id",
			set:      true,
			claims:   &utils.UserClaims{UserID: config.UserIdInitialVal, Role: model.UserRoleNormal},
			status:   http.StatusOK,
			wantNext: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.POST("/upgrade", func(c *gin.Context) {
				if tc.set {
					c.Set(config.AuthUserClaims, tc.claims)
				}
				c.Next()
			}, AdminAccess(), func(c *gin.Context) {
				c.Status(http.StatusOK)
			})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/upgrade", nil))
			if w.Code != tc.status {
				t.Fatalf("status=%d want %d body=%s", w.Code, tc.status, w.Body.String())
			}
			if tc.wantNext {
				return
			}
			var resp dto.Response
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp.Code != dto.FAILED {
				t.Fatalf("code=%d want %d", resp.Code, dto.FAILED)
			}
		})
	}
}
