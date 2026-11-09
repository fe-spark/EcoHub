package service

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
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
		if task.Model == model.TaskModelLogClean {
			found = true
			if task.Id != "sys_cron_log_clean" {
				t.Errorf("expected Id 'sys_cron_log_clean', got %s", task.Id)
			}
			if task.Spec != "0 10 3 * * *" {
				t.Errorf("expected Spec '0 10 3 * * *', got %s", task.Spec)
			}
			if !task.State {
				t.Errorf("expected State to be true, got false")
			}
			if task.Remark != "清理过期运行日志" {
				t.Errorf("expected Remark '清理过期运行日志', got %s", task.Remark)
			}
		}
	}
	if !found {
		t.Fatalf("Model TaskModelLogClean task not found in defaultFilmTasks()")
	}
}

func TestService_RedisNilSafety(t *testing.T) {
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
	tasks, err := svc.ensureDefaultTasks()
	if err != nil {
		t.Fatalf("ensureDefaultTasks: %v", err)
	}
	if len(tasks) != 4 {
		t.Fatalf("expected 4 default tasks, got %d", len(tasks))
	}
	collectClean, err := repository.GetFilmTaskById(model.TaskIDCollectClean)
	if err != nil {
		t.Fatalf("collect record clean cron: %v", err)
	}
	if collectClean.Model != model.TaskModelCollectClean || collectClean.Spec != "0 40 3 * * *" || !collectClean.State {
		t.Fatalf("unexpected collect clean task: %+v", collectClean)
	}
	task, err := repository.GetFilmTaskById("sys_cron_log_clean")
	if err != nil {
		t.Fatalf("failed to get sys_cron_log_clean: %v", err)
	}
	if task.Model != model.TaskModelLogClean || task.Spec != "0 10 3 * * *" || !task.State {
		t.Fatalf("unexpected task fields: %+v", task)
	}
}
