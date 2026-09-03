package service

import (
	"testing"

	"server/internal/config"
	"server/internal/spider"
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

func TestDefaultFilmTasks_SpecValid(t *testing.T) {
	for _, task := range defaultFilmTasks() {
		if err := spider.ValidSpec(task.Spec); err != nil {
			t.Fatalf("task [%s, model=%d] invalid spec %q: %v", task.Id, task.Model, task.Spec, err)
		}
	}
}
