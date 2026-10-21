package repository

import (
	"fmt"
	"testing"
	"time"

	"server/internal/config"
	"server/internal/infra/db"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestAuthRepo_RedisNilSafety(t *testing.T) {
	origRdb := db.Rdb
	db.Rdb = nil
	defer func() {
		db.Rdb = origRdb
	}()

	// 1. SaveUserToken with nil Rdb
	if err := SaveUserToken("test-token", 100); err != nil {
		t.Fatalf("expected nil err when db.Rdb is nil, got %v", err)
	}

	// 2. HasUserToken with nil Rdb
	if HasUserToken(100, "test-token") {
		t.Fatalf("expected false when db.Rdb is nil")
	}

	// 3. RemoveUserToken with nil Rdb
	if err := RemoveUserToken(100, "test-token"); err != nil {
		t.Fatalf("expected nil err when db.Rdb is nil, got %v", err)
	}

	// 4. ClearUserToken with nil Rdb
	if err := ClearUserToken(100); err != nil {
		t.Fatalf("expected nil err when db.Rdb is nil, got %v", err)
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

	if err := SaveUserToken("token-123", 42); err != nil {
		t.Fatalf("SaveUserToken failed: %v", err)
	}

	if !HasUserToken(42, "token-123") {
		t.Fatalf("expected token-123 to exist in redis")
	}
	ttl := mr.TTL(fmt.Sprintf(config.UserTokenKey, 42))
	want := config.AuthTokenExpires * time.Hour
	if ttl <= 0 || ttl > want || want-ttl > time.Minute {
		t.Fatalf("token ttl=%s, want about %s", ttl, want)
	}

	if err := ClearUserToken(42); err != nil {
		t.Fatalf("ClearUserToken failed: %v", err)
	}

	if HasUserToken(42, "token-123") {
		t.Fatalf("expected false after clear")
	}
}

func TestAuthRepo_MultiDeviceSupport(t *testing.T) {
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
	if err := SaveUserToken("token-device-a", 10000); err != nil {
		t.Fatalf("SaveUserToken A failed: %v", err)
	}
	// 2. 设备 B 登录
	if err := SaveUserToken("token-device-b", 10000); err != nil {
		t.Fatalf("SaveUserToken B failed: %v", err)
	}

	// 3. 两个设备的 Token 均处于有效状态，互不影响
	if !HasUserToken(10000, "token-device-a") {
		t.Fatalf("expected token-device-a to be valid")
	}
	if !HasUserToken(10000, "token-device-b") {
		t.Fatalf("expected token-device-b to be valid")
	}

	// 4. 设备 A 单点登出
	if err := RemoveUserToken(10000, "token-device-a"); err != nil {
		t.Fatalf("RemoveUserToken failed: %v", err)
	}
	if HasUserToken(10000, "token-device-a") {
		t.Fatalf("expected token-device-a to be removed")
	}
	// 设备 B 依然有效
	if !HasUserToken(10000, "token-device-b") {
		t.Fatalf("expected token-device-b to remain valid after device A logout")
	}

	// 5. 全量登出 / 修改密码清空
	if err := ClearUserToken(10000); err != nil {
		t.Fatalf("ClearUserToken failed: %v", err)
	}
	if HasUserToken(10000, "token-device-b") {
		t.Fatalf("expected token-device-b to be cleared")
	}
}
