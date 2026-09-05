package service

import (
	"context"
	"testing"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/notify"
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

func TestShouldMigrateOrphanCleanSpec(t *testing.T) {
	tests := []struct {
		id       string
		spec     string
		expected bool
	}{
		{"sys_cron_orphan_clean", "0 0 0 * * *", true},
		{"sys_cron_orphan_clean", " 0 0 0 * * * ", true},
		{"sys_cron_orphan_clean", config.OrphanCleanSpec, false},
		{"sys_cron_orphan_clean", "0 */30 * * * ?", false},
		{"other_task", "0 0 0 * * *", false},
		{"sys_cron_auto_collect", "0 0 0 * * *", false},
	}
	for _, tt := range tests {
		got := shouldMigrateOrphanCleanSpec(tt.id, tt.spec)
		if got != tt.expected {
			t.Errorf("shouldMigrateOrphanCleanSpec(%q, %q) = %v, want %v", tt.id, tt.spec, got, tt.expected)
		}
	}
}

func TestService_RedisNilSafety(t *testing.T) {
	// 等待前序并发测试可能派生的异步通知协程消费完毕，避免对全局 db.Rdb 产生数据竞态
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer drainCancel()
	_ = notify.WaitPendingPublishes(drainCtx)

	origRdb := db.Rdb
	db.Rdb = nil
	defer func() {
		db.Rdb = origRdb
	}()

	// 1. clearStartupCaches with nil Rdb
	clearStartupCaches()

	// 2. loadLatestRelease with nil Rdb (network failure returns error, but no panic on Redis)
	_, _ = VersionSvc.loadLatestRelease(false)
}
