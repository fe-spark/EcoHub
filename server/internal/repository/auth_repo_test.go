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

	if err := SaveUserToken("test-token", 100); err == nil {
		t.Fatal("expected error when db.Rdb is nil")
	}
	if _, err := GetUserToken(100); err == nil {
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

	if err := SaveUserToken("token-123", 42); err != nil {
		t.Fatalf("SaveUserToken failed: %v", err)
	}

	token, err := GetUserToken(42)
	if err != nil {
		t.Fatalf("GetUserToken failed: %v", err)
	}
	if token != "token-123" {
		t.Fatalf("expected token-123, got %q", token)
	}
	ttl := mr.TTL(fmt.Sprintf(config.UserTokenKey, 42))
	want := config.AuthTokenExpires * time.Hour
	if ttl <= 0 || ttl > want || want-ttl > time.Minute {
		t.Fatalf("token ttl=%s, want about %s", ttl, want)
	}

	if err := ClearUserToken(42); err != nil {
		t.Fatalf("ClearUserToken failed: %v", err)
	}

	token, err = GetUserToken(42)
	if err != nil {
		t.Fatalf("GetUserToken after clear failed: %v", err)
	}
	if token != "" {
		t.Fatalf("expected empty token after clear, got %q", token)
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
	if err := SaveUserToken("token-device-a", 10000); err != nil {
		t.Fatalf("SaveUserToken A failed: %v", err)
	}
	token, err := GetUserToken(10000)
	if err != nil {
		t.Fatalf("GetUserToken A failed: %v", err)
	}
	if token != "token-device-a" {
		t.Fatalf("expected token-device-a, got %q", token)
	}

	// 2. 设备 B 登录，覆盖顶掉设备 A 的登录状态
	if err := SaveUserToken("token-device-b", 10000); err != nil {
		t.Fatalf("SaveUserToken B failed: %v", err)
	}
	token, err = GetUserToken(10000)
	if err != nil {
		t.Fatalf("GetUserToken B failed: %v", err)
	}
	if token != "token-device-b" {
		t.Fatalf("expected token-device-b to supersede token-device-a, got %q", token)
	}

	// 3. 注销 / 修改密码清空
	if err := ClearUserToken(10000); err != nil {
		t.Fatalf("ClearUserToken failed: %v", err)
	}
	token, err = GetUserToken(10000)
	if err != nil {
		t.Fatalf("GetUserToken after clear failed: %v", err)
	}
	if token != "" {
		t.Fatalf("expected empty token after clear, got %q", token)
	}
}
