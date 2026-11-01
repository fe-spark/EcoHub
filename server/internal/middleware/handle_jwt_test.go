package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	"server/internal/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
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

func seedAuthUser(t *testing.T, id uint) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate user: %v", err)
	}
	user := model.User{
		Model:    gorm.Model{ID: id},
		UserName: "admin",
		Status:   0,
		Role:     model.UserRoleAdmin,
	}
	if err := gdb.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	orig := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = orig })
}

func TestAuthToken_SingleSignOn(t *testing.T) {
	seedAuthUser(t, 10086)
	origRdb := db.Rdb
	defer func() {
		db.Rdb = origRdb
	}()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	db.Rdb = client

	r := gin.New()
	r.GET("/api/manage/test", AuthToken(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"msg": "ok"})
	})

	// 1. 无凭证 -> 401
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest(http.MethodGet, "/api/manage/test", nil)
	r.ServeHTTP(w1, req1)
	if w1.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for no token, got %d", w1.Code)
	}

	// 2. 设备 A 登录
	tokenA, _ := utils.GenToken(10086, "admin", model.UserRoleAdmin)
	_ = repository.SaveUserToken(tokenA, 10086)

	// 设备 A 访问 -> 200
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/api/manage/test", nil)
	req2.Header.Set("Authorization", "Bearer "+tokenA)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 for tokenA, got %d", w2.Code)
	}

	// 3. 设备 B 登录，顶掉设备 A
	tokenB, _ := utils.GenToken(10086, "admin", model.UserRoleAdmin)
	_ = repository.SaveUserToken(tokenB, 10086)

	// 设备 A 再次访问 -> 401，提示其它设备登录
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest(http.MethodGet, "/api/manage/test", nil)
	req3.Header.Set("Authorization", "Bearer "+tokenA)
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for superseded tokenA, got %d", w3.Code)
	}
	var resp3 dto.Response
	_ = json.Unmarshal(w3.Body.Bytes(), &resp3)
	if resp3.Msg != "账号在其它设备登录,身份验证信息失效,请重新登录!!!" {
		t.Fatalf("unexpected message: %s", resp3.Msg)
	}

	// 设备 B 访问 -> 200
	w4 := httptest.NewRecorder()
	req4, _ := http.NewRequest(http.MethodGet, "/api/manage/test", nil)
	req4.Header.Set("Authorization", "Bearer "+tokenB)
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("expected 200 for tokenB, got %d", w4.Code)
	}

	// 4. 清理登录态（注销/改密）
	_ = repository.ClearUserToken(10086)
	w5 := httptest.NewRecorder()
	req5, _ := http.NewRequest(http.MethodGet, "/api/manage/test", nil)
	req5.Header.Set("Authorization", "Bearer "+tokenB)
	r.ServeHTTP(w5, req5)
	if w5.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after clear, got %d", w5.Code)
	}
}

func TestAuthToken_ReReadsRole(t *testing.T) {
	seedAuthUser(t, 10087)
	origRdb := db.Rdb
	defer func() { db.Rdb = origRdb }()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	db.Rdb = client

	token, err := utils.GenToken(10087, "admin", model.UserRoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveUserToken(token, 10087); err != nil {
		t.Fatal(err)
	}
	if err := db.Mdb.Model(&model.User{}).Where("id = ?", 10087).Update("role", model.UserRoleNormal).Error; err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.GET("/api/manage/config/access", AuthToken(), AdminAccess(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/manage/config/access", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("demoted token should be forbidden, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestForwardedProto(t *testing.T) {
	if got := forwardedProto("https, http"); got != "https" {
		t.Fatalf("got %q", got)
	}
	if got := forwardedProto("http"); got != "http" {
		t.Fatalf("got %q", got)
	}
	if got := forwardedProto("HTTPS"); got != "" {
		t.Fatalf("got %q", got)
	}
}
