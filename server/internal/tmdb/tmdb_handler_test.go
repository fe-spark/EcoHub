package tmdb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/testutil"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTMDBHandler_GetConfig_HidesApiKeyForNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.TMDBConfigRecord{}); err != nil {
		t.Fatal(err)
	}
	prevM, prevR := db.Mdb, db.Rdb
	db.Mdb, db.Rdb = gdb, nil
	t.Cleanup(func() { db.Mdb, db.Rdb = prevM, prevR })

	_ = TMDBSvc.UpdateConfig(model.TMDBConfig{
		Enabled: true,
		ApiKey:  "secret-tmdb-key-12345",
	})
	cases := []struct {
		name       string
		claims     any
		wantApiKey string
	}{
		{name: "anonymous", claims: nil, wantApiKey: ""},
		{
			name:       "visitor",
			claims:     &utils.UserClaims{UserID: 10002, UserName: "guest", Role: model.UserRoleVisitor},
			wantApiKey: "",
		},
		{
			name:       "normal user",
			claims:     &utils.UserClaims{UserID: 10003, UserName: "normal", Role: model.UserRoleNormal},
			wantApiKey: "",
		},
		{
			name:       "admin",
			claims:     &utils.UserClaims{UserID: config.UserIdInitialVal, UserName: "admin", Role: model.UserRoleAdmin},
			wantApiKey: MaskTMDBApiKey("secret-tmdb-key-12345"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/manage/tmdb/config", nil)
			if tc.claims != nil {
				c.Set(config.AuthUserClaims, tc.claims)
			}
			TMDBHd.GetConfig(c)
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d want 200", w.Code)
			}
			var resp dto.Response
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if resp.Code != dto.SUCCESS {
				t.Fatalf("code=%d want %d", resp.Code, dto.SUCCESS)
			}
			raw, _ := json.Marshal(resp.Data)
			var cfg model.TMDBConfig
			if err := json.Unmarshal(raw, &cfg); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if cfg.ApiKey != tc.wantApiKey {
				t.Fatalf("expected apiKey %q, got %q", tc.wantApiKey, cfg.ApiKey)
			}
			if !cfg.Configured {
				t.Fatalf("expected configured=true even when apiKey is hidden for %s", tc.name)
			}
		})
	}
}
