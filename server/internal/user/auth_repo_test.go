package user

import (
	"fmt"
	"testing"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestAuthRepo_RedisNilSafety(t *testing.T) {
	origRdb := db.Rdb
	db.Rdb = nil
	defer func() {
		db.Rdb = origRdb
	}()

	if err := SaveUserToken("test-token", 100, model.UserRoleAdmin); err == nil {
		t.Fatal("expected error when db.Rdb is nil")
	}
	if _, err := CheckUserToken(100, model.UserRoleAdmin, "test-token"); err == nil {
		t.Fatal("expected error when db.Rdb is nil")
	}
	if err := RevokeUserToken(100, model.UserRoleVisitor, "test-token"); err == nil {
		t.Fatal("expected error when db.Rdb is nil")
	}
	if err := ClearUserToken(100); err == nil {
		t.Fatal("expected error when db.Rdb is nil")
	}
}

func TestAuthRepo_WithRedis(t *testing.T) {
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

	if err := SaveUserToken("token-123", 42, model.UserRoleNormal); err != nil {
		t.Fatalf("SaveUserToken failed: %v", err)
	}

	status, err := CheckUserToken(42, model.UserRoleNormal, "token-123")
	if err != nil {
		t.Fatalf("CheckUserToken failed: %v", err)
	}
	if status != TokenValid {
		t.Fatalf("expected TokenValid, got %v", status)
	}
	ttl := mr.TTL(fmt.Sprintf(config.UserTokenKey, 42))
	want := config.AuthTokenExpires * time.Hour
	if ttl <= 0 || ttl > want || want-ttl > time.Minute {
		t.Fatalf("token ttl=%s, want about %s", ttl, want)
	}

	if err := ClearUserToken(42); err != nil {
		t.Fatalf("ClearUserToken failed: %v", err)
	}

	status, err = CheckUserToken(42, model.UserRoleNormal, "token-123")
	if err != nil {
		t.Fatalf("CheckUserToken after clear failed: %v", err)
	}
	if status != TokenMissing {
		t.Fatalf("expected TokenMissing after clear, got %v", status)
	}
}

func TestAuthRepo_SingleSignOn(t *testing.T) {
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

	// 1. 设备 A 登录
	if err := SaveUserToken("token-device-a", 10000, model.UserRoleAdmin); err != nil {
		t.Fatalf("SaveUserToken A failed: %v", err)
	}
	status, err := CheckUserToken(10000, model.UserRoleAdmin, "token-device-a")
	if err != nil || status != TokenValid {
		t.Fatalf("CheckUserToken A failed: status=%v err=%v", status, err)
	}

	// 2. 设备 B 登录，覆盖顶掉设备 A 的登录状态
	if err := SaveUserToken("token-device-b", 10000, model.UserRoleAdmin); err != nil {
		t.Fatalf("SaveUserToken B failed: %v", err)
	}
	status, err = CheckUserToken(10000, model.UserRoleAdmin, "token-device-a")
	if err != nil || status != TokenSuperseded {
		t.Fatalf("expected token-device-a superseded, got status=%v err=%v", status, err)
	}
	status, err = CheckUserToken(10000, model.UserRoleAdmin, "token-device-b")
	if err != nil || status != TokenValid {
		t.Fatalf("expected token-device-b valid, got status=%v err=%v", status, err)
	}

	// 3. 注销 / 修改密码清空
	if err := ClearUserToken(10000); err != nil {
		t.Fatalf("ClearUserToken failed: %v", err)
	}
	status, err = CheckUserToken(10000, model.UserRoleAdmin, "token-device-b")
	if err != nil || status != TokenMissing {
		t.Fatalf("expected TokenMissing after clear, got status=%v err=%v", status, err)
	}
}

func TestAuthRepo_VisitorMultiLogin(t *testing.T) {
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

	const visitorID uint = 20001

	// 1. 访客设备 A、B 同时登录，互不顶替
	if err := SaveUserToken("visitor-token-a", visitorID, model.UserRoleVisitor); err != nil {
		t.Fatalf("SaveUserToken A failed: %v", err)
	}
	if err := SaveUserToken("visitor-token-b", visitorID, model.UserRoleVisitor); err != nil {
		t.Fatalf("SaveUserToken B failed: %v", err)
	}

	if status, err := CheckUserToken(visitorID, model.UserRoleVisitor, "visitor-token-a"); err != nil || status != TokenValid {
		t.Fatalf("visitor token A should stay valid, got status=%v err=%v", status, err)
	}
	if status, err := CheckUserToken(visitorID, model.UserRoleVisitor, "visitor-token-b"); err != nil || status != TokenValid {
		t.Fatalf("visitor token B should be valid, got status=%v err=%v", status, err)
	}
	ttl := mr.TTL(visitorTokenKey(visitorID, 0, "visitor-token-a"))
	want := config.AuthTokenExpires * time.Hour
	if ttl <= 0 || ttl > want || want-ttl > time.Minute {
		t.Fatalf("visitor token ttl=%s, want about %s", ttl, want)
	}

	// 2. 设备 A 单独登出，不影响设备 B
	if err := RevokeUserToken(visitorID, model.UserRoleVisitor, "visitor-token-a"); err != nil {
		t.Fatalf("RevokeUserToken A failed: %v", err)
	}
	if status, err := CheckUserToken(visitorID, model.UserRoleVisitor, "visitor-token-a"); err != nil || status != TokenMissing {
		t.Fatalf("visitor token A should be missing after revoke, got status=%v err=%v", status, err)
	}
	if status, err := CheckUserToken(visitorID, model.UserRoleVisitor, "visitor-token-b"); err != nil || status != TokenValid {
		t.Fatalf("visitor token B should remain valid after A revokes, got status=%v err=%v", status, err)
	}

	// 3. 全量清除（改密/封禁/改角色）递增世代号，全部设备立即失效
	if err := ClearUserToken(visitorID); err != nil {
		t.Fatalf("ClearUserToken failed: %v", err)
	}
	if status, err := CheckUserToken(visitorID, model.UserRoleVisitor, "visitor-token-b"); err != nil || status != TokenMissing {
		t.Fatalf("visitor token B should be invalidated after ClearUserToken, got status=%v err=%v", status, err)
	}

	// 4. 新世代重新登录设备 C 有效，旧世代设备 A/B 仍不可用
	if err := SaveUserToken("visitor-token-c", visitorID, model.UserRoleVisitor); err != nil {
		t.Fatalf("SaveUserToken C failed: %v", err)
	}
	if status, err := CheckUserToken(visitorID, model.UserRoleVisitor, "visitor-token-c"); err != nil || status != TokenValid {
		t.Fatalf("visitor token C should be valid, got status=%v err=%v", status, err)
	}
	if status, err := CheckUserToken(visitorID, model.UserRoleVisitor, "visitor-token-b"); err != nil || status != TokenMissing {
		t.Fatalf("old generation visitor token B must remain invalid, got status=%v err=%v", status, err)
	}
}
