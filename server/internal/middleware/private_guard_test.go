package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

func TestProvideKeyGuard(t *testing.T) {
	// 默认未开启私有化，无论是否有 key 都应放行
	r := gin.New()
	r.Use(ProvideKeyGuard())
	r.GET("/api/provide/config", func(c *gin.Context) {
		dto.Success("ok", "success", c)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/provide/config", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	// 模拟开启私有化并设置 key
	if db.Rdb != nil {
		testCfg := model.BasicConfig{
			PrivateAccess: true,
			ProvideKey:    "test-secret-123",
		}
		data, _ := json.Marshal(testCfg)
		_ = db.Rdb.Set(db.Cxt, config.SiteConfigBasic, data, 0).Err()
		// 0. 未配置 key 时访问 -> 403
		emptyKeyCfg := model.BasicConfig{
			PrivateAccess: true,
			ProvideKey:    "",
		}
		data0, _ := json.Marshal(emptyKeyCfg)
		_ = db.Rdb.Set(db.Cxt, config.SiteConfigBasic, data0, 0).Err()
		w0 := httptest.NewRecorder()
		req0, _ := http.NewRequest(http.MethodGet, "/api/provide/config", nil)
		r.ServeHTTP(w0, req0)
		if w0.Code != http.StatusForbidden {
			t.Errorf("expected status 403 when provideKey is empty, got %d", w0.Code)
		}

		_ = db.Rdb.Set(db.Cxt, config.SiteConfigBasic, data, 0).Err()

		// 1. 无 key -> 401
		w1 := httptest.NewRecorder()
		req1, _ := http.NewRequest(http.MethodGet, "/api/provide/config", nil)
		r.ServeHTTP(w1, req1)
		if w1.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", w1.Code)
		}

		// 2. 错误 key -> 403
		w2 := httptest.NewRecorder()
		req2, _ := http.NewRequest(http.MethodGet, "/api/provide/config?key=wrong", nil)
		r.ServeHTTP(w2, req2)
		if w2.Code != http.StatusForbidden {
			t.Errorf("expected status 403, got %d", w2.Code)
		}

		// 3. 正确 query key -> 200
		w3 := httptest.NewRecorder()
		req3, _ := http.NewRequest(http.MethodGet, "/api/provide/config?key=test-secret-123", nil)
		r.ServeHTTP(w3, req3)
		if w3.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w3.Code)
		}

		// 4. 正确 header key -> 200
		w4 := httptest.NewRecorder()
		req4, _ := http.NewRequest(http.MethodGet, "/api/provide/config", nil)
		req4.Header.Set("X-Provide-Key", "test-secret-123")
		r.ServeHTTP(w4, req4)
		if w4.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w4.Code)
		}
	}
}

func TestPrivateAccessGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(PrivateAccessGuard())
	r.GET("/api/test-front", func(c *gin.Context) {
		uid, _ := c.Get("user_id")
		dto.Success(uid, "ok", c)
	})

	// 1. 未开启私有化，无任何凭证应直接通过
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/test-front", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 when private access disabled, got %d", w.Code)
	}

	if db.Rdb != nil {
		testCfg := model.BasicConfig{
			PrivateAccess: true,
			ProvideKey:    "secret-123",
		}
		data, _ := json.Marshal(testCfg)
		_ = db.Rdb.Set(db.Cxt, config.SiteConfigBasic, data, 0).Err()

		// 2. 开启私有化，无凭据访问 -> 401
		w1 := httptest.NewRecorder()
		req1, _ := http.NewRequest(http.MethodGet, "/api/test-front", nil)
		r.ServeHTTP(w1, req1)
		if w1.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 without auth, got %d", w1.Code)
		}

		// 3. 携带正确订阅密钥 -> 200
		w2 := httptest.NewRecorder()
		req2, _ := http.NewRequest(http.MethodGet, "/api/test-front?key=secret-123", nil)
		r.ServeHTTP(w2, req2)
		if w2.Code != http.StatusOK {
			t.Errorf("expected 200 with provide key, got %d", w2.Code)
		}

		// 4. 携带仅 JWT 签名合法但 Redis 无凭据的伪造/注销 Token -> 401，且携带 Cookie 时清理失效 Cookie
		forgedToken, _ := utils.GenToken(999, "fakeUser", 2)
		w3 := httptest.NewRecorder()
		req3, _ := http.NewRequest(http.MethodGet, "/api/test-front", nil)
		req3.AddCookie(&http.Cookie{Name: config.AuthCookieName, Value: forgedToken})
		r.ServeHTTP(w3, req3)
		if w3.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 for token not present in redis, got %d", w3.Code)
		}
		cleared := false
		for _, ck := range w3.Result().Cookies() {
			if ck.Name == config.AuthCookieName && ck.MaxAge < 0 {
				cleared = true
				break
			}
		}
		if !cleared {
			t.Errorf("expected stale %s cookie to be cleared on 401", config.AuthCookieName)
		}

		// 4b. 仅携带 Authorization 头且无 Cookie 时，不额外下发清除 Cookie 头
		w3b := httptest.NewRecorder()
		req3b, _ := http.NewRequest(http.MethodGet, "/api/test-front", nil)
		req3b.Header.Set("Authorization", "Bearer "+forgedToken)
		r.ServeHTTP(w3b, req3b)
		if w3b.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 for forged bearer token, got %d", w3b.Code)
		}
		for _, ck := range w3b.Result().Cookies() {
			if ck.Name == config.AuthCookieName {
				t.Errorf("did not expect Set-Cookie for %s when request carried no cookie", config.AuthCookieName)
			}
		}

		// 5. 携带 Redis 同步有效的合法 Token -> 200，且 user_id 为 uint 999
		_ = repository.SaveUserToken(forgedToken, 999)
		w4 := httptest.NewRecorder()
		req4, _ := http.NewRequest(http.MethodGet, "/api/test-front", nil)
		req4.Header.Set("Authorization", "Bearer "+forgedToken)
		r.ServeHTTP(w4, req4)
		if w4.Code != http.StatusOK {
			t.Errorf("expected 200 for valid token in redis, got %d", w4.Code)
		}
		_ = repository.ClearUserToken(999)
	}
}
