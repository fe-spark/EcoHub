package service

import (
	"context"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/notify"
	"server/internal/repository"
	"server/internal/spider"
)

func TestDefaultFilmTasks_SpecValid(t *testing.T) {
	for _, task := range defaultFilmTasks() {
		if err := spider.ValidSpec(task.Spec); err != nil {
			t.Fatalf("task [%s, model=%d] invalid spec %q: %v", task.Id, task.Model, task.Spec, err)
		}
	}
}

func TestDefaultFilmTasks_ContainsLogClean(t *testing.T) {
	tasks := defaultFilmTasks()
	var found bool
	for _, task := range tasks {
		if task.Model == 4 {
			found = true
			if task.Id != "sys_cron_log_clean" {
				t.Errorf("expected Id 'sys_cron_log_clean', got %s", task.Id)
			}
			if task.Spec != "0 0 3 * * *" {
				t.Errorf("expected Spec '0 0 3 * * *', got %s", task.Spec)
			}
			if !task.State {
				t.Errorf("expected State to be true, got false")
			}
			if task.Remark != "自动清理过期运行日志" {
				t.Errorf("expected Remark '自动清理过期运行日志', got %s", task.Remark)
			}
		}
	}
	if !found {
		t.Fatalf("Model 4 log clean task not found in defaultFilmTasks()")
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

	// 2. LoadLatestRelease with nil Rdb (network failure returns error, but no panic on Redis)
	_, _ = VersionSvc.LoadLatestRelease(false)
}

func TestEnsureDefaultTasks_CleanInstall(t *testing.T) {
	setupTestDBAndRedis(t)
	svc := &InitService{}
	tasks := svc.ensureDefaultTasks()
	if len(tasks) != 5 {
		t.Fatalf("expected 5 default tasks, got %d", len(tasks))
	}
	task, err := repository.GetFilmTaskById("sys_cron_log_clean")
	if err != nil {
		t.Fatalf("failed to get sys_cron_log_clean: %v", err)
	}
	if task.Model != 4 || task.Spec != "0 0 3 * * *" || !task.State {
		t.Fatalf("unexpected task fields: %+v", task)
	}
}
