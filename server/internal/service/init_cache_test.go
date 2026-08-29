package service

import (
	"testing"

	"server/internal/config"
)

func TestShouldRetainStartupRedisKey(t *testing.T) {
	if !shouldRetainStartupRedisKey(config.RedisKeyPrefix + ":User:Token:1") {
		t.Fatal("login token must be retained")
	}
	if !shouldRetainStartupRedisKey(config.NotifyBotPollerLockKey) {
		t.Fatal("bot poller lock must be retained")
	}
	if !shouldRetainStartupRedisKey(config.AccessKeyPrefix + "day:20260829") {
		t.Fatal("access analysis keys must survive restart")
	}
	if !shouldRetainStartupRedisKey(config.AccessKeyPrefix + "recent") {
		t.Fatal("access recent list must survive restart")
	}
	if shouldRetainStartupRedisKey(config.RedisKeyPrefix + ":Index:Page") {
		t.Fatal("ordinary cache keys must still be purged")
	}
}
