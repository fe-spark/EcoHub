package service

import (
	"context"
	"testing"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
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

	// 2. loadLatestRelease with nil Rdb (network failure returns error, but no panic on Redis)
	_, _ = VersionSvc.loadLatestRelease(false)
}

func TestEnsureDefaultTasks_CleanInstall(t *testing.T) {
	setupTestDBAndRedis(t)
	svc := &InitService{}
	tasks := svc.ensureDefaultTasks()
	if len(tasks) != 4 {
		t.Fatalf("expected 4 default tasks, got %d", len(tasks))
	}
	task, err := repository.GetFilmTaskById("sys_cron_log_clean")
	if err != nil {
		t.Fatalf("failed to get sys_cron_log_clean: %v", err)
	}
	if task.Model != 4 || task.Spec != "0 0 3 * * *" || !task.State {
		t.Fatalf("unexpected task fields: %+v", task)
	}
}

func TestEnsureDefaultTasks_MigrateLegacyApiLogClean(t *testing.T) {
	setupTestDBAndRedis(t)
	legacy := model.FilmCollectTask{
		Id:     "sys_cron_api_log_clean",
		Model:  4,
		State:  false,
		Spec:   "0 30 4 * * *",
		Remark: "旧日志清理",
	}
	if err := repository.SaveFilmTask(legacy); err != nil {
		t.Fatalf("save legacy task: %v", err)
	}

	svc := &InitService{}
	tasks := svc.ensureDefaultTasks()
	if len(tasks) != 4 {
		t.Fatalf("expected 4 tasks, got %d", len(tasks))
	}

	if _, err := repository.GetFilmTaskById("sys_cron_api_log_clean"); err == nil {
		t.Fatalf("expected sys_cron_api_log_clean to be deleted")
	}

	migrated, err := repository.GetFilmTaskById("sys_cron_log_clean")
	if err != nil {
		t.Fatalf("expected sys_cron_log_clean to exist: %v", err)
	}
	if migrated.Model != 4 {
		t.Errorf("expected Model 4, got %d", migrated.Model)
	}
	if migrated.State != false {
		t.Errorf("expected State false, got %v", migrated.State)
	}
	if migrated.Spec != "0 30 4 * * *" {
		t.Errorf("expected Spec '0 30 4 * * *', got %s", migrated.Spec)
	}
	if migrated.Remark != "自动清理过期运行日志" {
		t.Errorf("expected Remark '自动清理过期运行日志', got %s", migrated.Remark)
	}
}

func TestEnsureDefaultTasks_BothLegacyAndCanonical(t *testing.T) {
	setupTestDBAndRedis(t)
	canonical := model.FilmCollectTask{
		Id:     "sys_cron_log_clean",
		Model:  4,
		State:  false,
		Spec:   "0 15 2 * * *",
		Remark: "自定义日志清理",
	}
	legacy := model.FilmCollectTask{
		Id:     "sys_cron_api_log_clean",
		Model:  4,
		State:  true,
		Spec:   "0 0 3 * * *",
		Remark: "老旧冗余清理",
	}
	if err := repository.SaveFilmTask(canonical); err != nil {
		t.Fatalf("save canonical: %v", err)
	}
	if err := repository.SaveFilmTask(legacy); err != nil {
		t.Fatalf("save legacy: %v", err)
	}

	svc := &InitService{}
	tasks := svc.ensureDefaultTasks()
	if len(tasks) != 4 {
		t.Fatalf("expected 4 tasks, got %d", len(tasks))
	}

	if _, err := repository.GetFilmTaskById("sys_cron_api_log_clean"); err == nil {
		t.Fatalf("expected sys_cron_api_log_clean to be deleted")
	}

	c, err := repository.GetFilmTaskById("sys_cron_log_clean")
	if err != nil {
		t.Fatalf("sys_cron_log_clean must exist: %v", err)
	}
	if c.Spec != "0 15 2 * * *" || c.State != false {
		t.Errorf("expected canonical settings preserved, got spec=%s state=%v", c.Spec, c.State)
	}

	tasks2 := svc.ensureDefaultTasks()
	if len(tasks2) != 4 {
		t.Fatalf("expected 4 tasks on second run, got %d", len(tasks2))
	}
	if _, err := repository.GetFilmTaskById("sys_cron_log_clean"); err != nil {
		t.Fatalf("sys_cron_log_clean must still exist after second run: %v", err)
	}
}

func TestEnsureDefaultTasks_MultipleModel4Deduplication(t *testing.T) {
	setupTestDBAndRedis(t)
	extra1 := model.FilmCollectTask{Id: "extra_clean_1", Model: 4, State: true, Spec: "0 0 1 * * *"}
	extra2 := model.FilmCollectTask{Id: "extra_clean_2", Model: 4, State: false, Spec: "0 0 2 * * *"}
	canonical := model.FilmCollectTask{Id: "sys_cron_log_clean", Model: 4, State: true, Spec: "0 0 3 * * *"}
	_ = repository.SaveFilmTask(extra1)
	_ = repository.SaveFilmTask(extra2)
	_ = repository.SaveFilmTask(canonical)

	svc := &InitService{}
	tasks := svc.ensureDefaultTasks()
	if len(tasks) != 4 {
		t.Fatalf("expected 4 tasks, got %d", len(tasks))
	}

	if _, err := repository.GetFilmTaskById("extra_clean_1"); err == nil {
		t.Fatalf("expected extra_clean_1 to be deleted")
	}
	if _, err := repository.GetFilmTaskById("extra_clean_2"); err == nil {
		t.Fatalf("expected extra_clean_2 to be deleted")
	}
	if _, err := repository.GetFilmTaskById("sys_cron_log_clean"); err != nil {
		t.Fatalf("expected sys_cron_log_clean to exist: %v", err)
	}
}
