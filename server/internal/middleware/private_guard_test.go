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

		// 1. 无 key -> 403
		w1 := httptest.NewRecorder()
		req1, _ := http.NewRequest(http.MethodGet, "/api/provide/config", nil)
		r.ServeHTTP(w1, req1)
		if w1.Code != http.StatusForbidden {
			t.Errorf("expected status 403, got %d", w1.Code)
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
