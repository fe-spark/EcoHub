package user

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/testutil"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupUserHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })
	return gdb
}

func testJSONUserContext(method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, w
}

func TestUserHandler_LoginAccountLimiterAndUnifiedError(t *testing.T) {
	setupUserHandlerTestDB(t)
	loginFailMu.Lock()
	loginFailState = map[string]loginAttemptState{}
	loginFailMu.Unlock()

	for i := 0; i < 5; i++ {
		c, w := testJSONUserContext(http.MethodPost, "/api/login", `{"userName":"nobody_user","password":"bad"}`)
		c.Request.RemoteAddr = "203.0.113.10:12345"
		UserHd.Login(c)
		var resp dto.Response
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Code != dto.FAILED || resp.Msg != "用户名或密码错误" {
			t.Fatalf("attempt %d: got code=%d msg=%q, want 用户名或密码错误", i+1, resp.Code, resp.Msg)
		}
	}

	// 同 IP 第 6 次失败应被锁定；同时验证不同合法 IP 不会被恶意锁定同一账号
	c6, w6 := testJSONUserContext(http.MethodPost, "/api/login", `{"userName":"nobody_user","password":"bad"}`)
	c6.Request.RemoteAddr = "203.0.113.10:12345"
	UserHd.Login(c6)
	var resp6 dto.Response
	if err := json.Unmarshal(w6.Body.Bytes(), &resp6); err != nil {
		t.Fatal(err)
	}
	if resp6.Code != dto.FAILED || !strings.Contains(resp6.Msg, "频繁") {
		t.Fatalf("expected ip+account rate limit on 6th attempt, got code=%d msg=%q", resp6.Code, resp6.Msg)
	}

	cOther, wOther := testJSONUserContext(http.MethodPost, "/api/login", `{"userName":"nobody_user","password":"bad"}`)
	cOther.Request.RemoteAddr = "203.0.113.99:12345"
	UserHd.Login(cOther)
	var respOther dto.Response
	if err := json.Unmarshal(wOther.Body.Bytes(), &respOther); err != nil {
		t.Fatal(err)
	}
	if respOther.Msg != "用户名或密码错误" {
		t.Fatalf("expected other IP not locked by global account DoS, got %q", respOther.Msg)
	}
}

func TestReserveLoginAttempts_ParallelBurstStopsAtFive(t *testing.T) {
	loginFailMu.Lock()
	loginFailState = map[string]loginAttemptState{}
	loginFailMu.Unlock()

	var wg sync.WaitGroup
	var allowed atomic.Int32
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if reserveLoginAttempts("ip|203.0.113.1", "ipacct|203.0.113.1|burst") {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != 5 {
		t.Fatalf("parallel burst allowed %d, want 5", got)
	}
}

func TestReserveLoginAttempts_EvictsActiveWhenFullAndLimitsIP(t *testing.T) {
	loginFailMu.Lock()
	loginFailState = map[string]loginAttemptState{}
	loginFailMu.Unlock()

	for i := 0; i < loginMaxEntries+50; i++ {
		if !reserveLoginAttempts("", fmt.Sprintf("ipacct|198.51.100.%d|u%d", i%200, i)) {
			t.Fatalf("unexpected limit at i=%d", i)
		}
	}
	loginFailMu.Lock()
	size := len(loginFailState)
	loginFailMu.Unlock()
	if size >= loginMaxEntries {
		t.Fatalf("expected active eviction keeping map < %d, got %d", loginMaxEntries, size)
	}

	for i := 0; i < loginMaxIPFails; i++ {
		if !reserveLoginAttempts("ip|198.51.100.88", fmt.Sprintf("ipacct|198.51.100.88|rand_%d", i)) {
			t.Fatalf("unexpected ip block at i=%d", i)
		}
	}
	if reserveLoginAttempts("ip|198.51.100.88", "ipacct|198.51.100.88|rand_next") {
		t.Fatal("expected pure IP rate limit to block distinct username spraying")
	}
}
