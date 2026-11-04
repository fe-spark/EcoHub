package spider

import (
	"context"
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

func TestExecuteOrphanCleanTask_YieldsInFlightCollect(t *testing.T) {
	prev := maintenanceExitWait
	maintenanceExitWait = 300 * time.Millisecond
	defer func() { maintenanceExitWait = prev }()

	ft := model.FilmCollectTask{
		Id:     "test_sys_orphan_clean",
		Model:  model.TaskModelOrphanClean,
		State:  true,
		Remark: "测试采集让行",
	}

	if err := collectLifecycle.beginSource("test_busy_src"); err != nil {
		t.Fatalf("beginSource failed: %v", err)
	}
	go func() {
		time.Sleep(40 * time.Millisecond)
		collectLifecycle.endSource("test_busy_src")
	}()
	executeOrphanCleanTask(ft)

	if err := collectLifecycle.beginSource("after_clean"); err != nil {
		t.Fatalf("collect should resume after clean: %v", err)
	}
	collectLifecycle.endSource("after_clean")
}

func TestRejectManualRun(t *testing.T) {
	if err := collectLifecycle.beginMaintenance(); err != nil {
		t.Fatal(err)
	}
	defer collectLifecycle.endDrain()

	collect := model.FilmCollectTask{Id: "collect", Model: model.TaskModelAutoCollect}
	if err := rejectManualRun(collect); !errors.Is(err, errCollectDuringMaintenance) {
		t.Fatalf("collect = %v", err)
	}
	orphan := model.FilmCollectTask{Id: "orphan", Model: model.TaskModelOrphanClean}
	if err := rejectManualRun(orphan); !errors.Is(err, errMaintenanceBusy) {
		t.Fatalf("maintenance = %v", err)
	}
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
	finishMaintenance(model.FilmCollectTask{Id: "sys_cron_orphan_clean"}, "片库清理", errors.New("刷新读模型失败"))
	if reason != "刷新读模型失败" {
		t.Fatalf("expected executed-failure notify, got %q", reason)
	}
}

func TestFinishMaintenance_BusyDoesNotNotify(t *testing.T) {
	prev := getNotifyDispatcher()
	defer RegisterNotifyDispatcher(prev)
	called := false
	RegisterNotifyDispatcher(NotifyDispatcher{
		OnCronFailed: func(string, string, string) { called = true },
	})
	finishMaintenance(model.FilmCollectTask{Id: "sys_cron_orphan_clean"}, "片库清理", errMaintenanceBusy)
	if called {
		t.Fatal("busy skip must not notify")
	}
}

func TestFinishMaintenance_WriteWaitNotifies(t *testing.T) {
	prev := getNotifyDispatcher()
	defer RegisterNotifyDispatcher(prev)
	var reason string
	RegisterNotifyDispatcher(NotifyDispatcher{
		OnCronFailed: func(_, _, got string) { reason = got },
	})
	err := fmt.Errorf("等待在途写入结束失败: %w", context.DeadlineExceeded)
	finishMaintenance(model.FilmCollectTask{Id: "sys_cron_orphan_clean"}, "片库清理", err)
	if reason != err.Error() {
		t.Fatalf("reason = %q", reason)
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

	run, skippedCollect, skippedMaintenance := planCronBatch([]model.FilmCollectTask{collect, logClean, orphan, collect}, false)
	if len(skippedCollect) != 1 || skippedCollect[0] != "collect" || len(skippedMaintenance) != 0 {
		t.Fatalf("skipped collect=%v maintenance=%v", skippedCollect, skippedMaintenance)
	}
	if len(run) != 2 || run[0].Id != "log" || run[1].Id != "orphan" {
		t.Fatalf("run = %+v", run)
	}

	run, skippedCollect, skippedMaintenance = planCronBatch([]model.FilmCollectTask{collect, logClean}, true)
	if len(run) != 0 || len(skippedCollect) != 1 || skippedCollect[0] != "collect" || len(skippedMaintenance) != 1 || skippedMaintenance[0] != "log" {
		t.Fatalf("busy run=%+v collect=%v maintenance=%v", run, skippedCollect, skippedMaintenance)
	}

	run, skippedCollect, skippedMaintenance = planCronBatch([]model.FilmCollectTask{collect, collect}, false)
	if len(skippedCollect) != 0 || len(skippedMaintenance) != 0 || len(run) != 1 || run[0].Id != "collect" {
		t.Fatalf("collect only run=%+v collect=%v maintenance=%v", run, skippedCollect, skippedMaintenance)
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
