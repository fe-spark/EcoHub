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
			if task.Remark != "清理过期运行日志" {
				t.Errorf("expected Remark '清理过期运行日志', got %s", task.Remark)
			}
		}
	}
	if !found {
		t.Fatalf("Model 4 log clean task not found in defaultFilmTasks()")
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
	tasks := svc.ensureDefaultTasks()
	if len(tasks) != 6 {
		t.Fatalf("expected 6 default tasks, got %d", len(tasks))
	}
	failureClean, err := repository.GetFilmTaskById(model.TaskIDFailureClean)
	if err != nil {
		t.Fatalf("failure record clean cron: %v", err)
	}
	if failureClean.Model != model.TaskModelFailureClean || failureClean.Spec != "0 30 3 * * *" || !failureClean.State {
		t.Fatalf("unexpected failure clean task: %+v", failureClean)
	}
	banner, err := repository.GetFilmTaskById(model.TaskIDBannerAuto)
	if err != nil {
		t.Fatalf("banner cron: %v", err)
	}
	if banner.Model != model.TaskModelBannerAuto || banner.Spec != "0 0 4 * * *" || banner.State {
		t.Fatalf("unexpected banner task: %+v", banner)
	}
	task, err := repository.GetFilmTaskById("sys_cron_log_clean")
	if err != nil {
		t.Fatalf("failed to get sys_cron_log_clean: %v", err)
	}
	if task.Model != 4 || task.Spec != "0 0 3 * * *" || !task.State {
		t.Fatalf("unexpected task fields: %+v", task)
	}
}
