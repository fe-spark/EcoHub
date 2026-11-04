package spider

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"server/internal/model"
)

func TestAddLogCleanCron_ValidAndInvalid(t *testing.T) {
	// Valid spec
	cid, err := AddLogCleanCron("test_log_clean", "0 0 3 * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cid < 0 {
		t.Fatalf("expected valid entry id, got %v", cid)
	}
	CronCollect.Remove(cid)

	// Invalid spec
	_, err = AddLogCleanCron("test_log_clean_invalid", "invalid-spec")
	if err == nil {
		t.Fatalf("expected error on invalid spec, got nil")
	}
}

func TestExecuteLogCleanTask(t *testing.T) {
	ft := model.FilmCollectTask{
		Id:     "test_sys_log_clean",
		Model:  4,
		State:  true,
		Remark: "自动清理过期运行日志",
	}
	// Should execute without panic
	executeLogCleanTask(ft)

	// Test with empty remark fallback
	ft2 := model.FilmCollectTask{
		Id:     "test_sys_log_clean_empty_remark",
		Model:  4,
		State:  true,
		Remark: "",
	}
	executeLogCleanTask(ft2)
}

func TestExecuteOrphanCleanTask_LowFrequencyPriority(t *testing.T) {
	origTimeout := orphanCleanWaitTimeout
	defer func() { orphanCleanWaitTimeout = origTimeout }()
	orphanCleanWaitTimeout = 300 * time.Millisecond

	ft := model.FilmCollectTask{
		Id:     "test_sys_orphan_clean",
		Model:  2,
		State:  true,
		Remark: "测试低频清理优先",
	}

	executeOrphanCleanTask(ft)

	if err := collectLifecycle.beginSource("test_busy_src"); err != nil {
		t.Fatalf("beginSource failed: %v", err)
	}
	rejected := make(chan error, 1)
	go func() {
		time.Sleep(40 * time.Millisecond)
		rejected <- collectLifecycle.beginSource("test_late_src")
	}()
	time.AfterFunc(80*time.Millisecond, func() {
		collectLifecycle.endSource("test_busy_src")
	})
	start := time.Now()
	executeOrphanCleanTask(ft)
	if time.Since(start) < 60*time.Millisecond {
		t.Errorf("expected to wait for in-flight collect, returned in %v", time.Since(start))
	}
	if err := <-rejected; err == nil {
		collectLifecycle.endSource("test_late_src")
		t.Fatal("new collect should be rejected while low-frequency clean holds the gate")
	}

	if err := collectLifecycle.beginSource("after_clean"); err != nil {
		t.Fatalf("collect should resume after clean: %v", err)
	}
	collectLifecycle.endSource("after_clean")

	orphanCleanWaitTimeout = 40 * time.Millisecond
	if err := collectLifecycle.beginSource("test_stuck_src"); err != nil {
		t.Fatalf("beginSource failed: %v", err)
	}
	defer collectLifecycle.endSource("test_stuck_src")
	executeOrphanCleanTask(ft)
}

func TestFinishOrphanClean_NotifiesExecutedFailure(t *testing.T) {
	prev := getNotifyDispatcher()
	defer RegisterNotifyDispatcher(prev)
	var reason string
	RegisterNotifyDispatcher(NotifyDispatcher{
		OnCronFailed: func(taskID, remark, got string) {
			if taskID != "sys_cron_orphan_clean" || remark != "片库清理" {
				t.Errorf("notify identity = %s %s", taskID, remark)
			}
			reason = got
		},
	})
	finishOrphanClean(model.FilmCollectTask{Id: "sys_cron_orphan_clean"}, "片库清理", time.Now(), errors.New("刷新读模型失败"))
	if reason != "刷新读模型失败" {
		t.Fatalf("expected executed-failure notify, got %q", reason)
	}
}

func TestFinishOrphanClean_DrainTimeoutDoesNotNotify(t *testing.T) {
	prev := getNotifyDispatcher()
	defer RegisterNotifyDispatcher(prev)
	called := false
	RegisterNotifyDispatcher(NotifyDispatcher{
		OnCronFailed: func(string, string, string) { called = true },
	})
	finishOrphanClean(model.FilmCollectTask{Id: "sys_cron_orphan_clean"}, "片库清理", time.Now(), fmt.Errorf("%w: active=1", errDrainTimeout))
	if called {
		t.Fatal("drain timeout must not notify")
	}
}

func TestTaskPrevIncludesManualRun(t *testing.T) {
	id := "test_manual_prev"
	taskRanAt.Delete(id)
	if !TaskPrev(id).IsZero() {
		t.Fatal("expected no previous run")
	}
	at := time.Now().Add(-time.Second)
	noteTaskRan(id, at)
	got := TaskPrev(id)
	if !got.Equal(at) {
		t.Fatalf("manual run = %v, want %v", got, at)
	}
}

func TestPlanCronBatchSkipsCollectWhenMaintenanceDue(t *testing.T) {
	collect := model.FilmCollectTask{Id: "collect", Model: model.TaskModelAutoCollect, State: true}
	logClean := model.FilmCollectTask{Id: "log", Model: model.TaskModelLogClean, State: true}
	orphan := model.FilmCollectTask{Id: "orphan", Model: model.TaskModelOrphanClean, State: true}

	run, skipped := planCronBatch([]model.FilmCollectTask{collect, logClean, orphan, collect}, false)
	if len(skipped) != 1 || skipped[0] != "collect" {
		t.Fatalf("skipped = %v", skipped)
	}
	if len(run) != 2 || run[0].Id != "log" || run[1].Id != "orphan" {
		t.Fatalf("run = %+v", run)
	}

	run, skipped = planCronBatch([]model.FilmCollectTask{collect}, true)
	if len(run) != 0 || len(skipped) != 1 || skipped[0] != "collect" {
		t.Fatalf("busy run=%+v skipped=%v", run, skipped)
	}

	run, skipped = planCronBatch([]model.FilmCollectTask{collect, collect}, false)
	if len(skipped) != 0 || len(run) != 1 || run[0].Id != "collect" {
		t.Fatalf("collect only run=%+v skipped=%v", run, skipped)
	}
}

func TestEnqueueCronTaskCoalesce(t *testing.T) {
	prevWait := cronCoalesceWait
	cronCoalesceWait = 30 * time.Millisecond
	defer func() { cronCoalesceWait = prevWait }()

	task := model.FilmCollectTask{Id: "coalesce_collect", Model: model.TaskModelAutoCollect, State: true}
	enqueueCronTask(task)
	enqueueCronTask(task)
	time.Sleep(60 * time.Millisecond)

	cronMu.Lock()
	pendingLen := len(cronPending)
	cronMu.Unlock()
	if pendingLen != 0 {
		t.Fatalf("pending tasks should be flushed, got %d", pendingLen)
	}
}
