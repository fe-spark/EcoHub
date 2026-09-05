package repository

import (
	"testing"

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

	// 2. GetUserTokenById with nil Rdb
	if token := GetUserTokenById(100); token != "" {
		t.Fatalf("expected empty token when db.Rdb is nil, got %q", token)
	}

	// 3. ClearUserToken with nil Rdb
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

	token := GetUserTokenById(42)
	if token != "token-123" {
		t.Fatalf("expected token-123, got %q", token)
	}

	if err := ClearUserToken(42); err != nil {
		t.Fatalf("ClearUserToken failed: %v", err)
	}

	if token := GetUserTokenById(42); token != "" {
		t.Fatalf("expected empty token after clear, got %q", token)
	}
}
