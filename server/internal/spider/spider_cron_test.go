package spider

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
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

func TestEnqueueCronTaskCoalesce(t *testing.T) {
	prevWait := cronCoalesceWaitNs.Swap(int64(30 * time.Millisecond))
	defer cronCoalesceWaitNs.Store(prevWait)

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

func TestExecuteTask_AutoCollectOverlapRunsOneFollowUp(t *testing.T) {
	const id = "test_cron_rerun"
	prev := autoCollectFn
	t.Cleanup(func() {
		autoCollectFn = prev
		runningCronTasks.Delete(id)
		cronCollectRerun.Delete(id)
	})

	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	autoCollectFn = func(string, int) error {
		n := calls.Add(1)
		if n == 1 {
			close(started)
			<-release
			return nil
		}
		noteCronCollectRerun(id)
		return nil
	}

	ft := model.FilmCollectTask{Id: id, Model: model.TaskModelAutoCollect, State: true, Time: 24}
	done := make(chan struct{})
	go func() {
		executeTask(ft, false)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first collect did not start")
	}
	executeTask(ft, false)
	executeTask(ft, false)
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("collect did not finish")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2", got)
	}
	if _, ok := cronCollectRerun.Load(id); ok {
		t.Fatal("ticks during the follow-up must not start another queue")
	}
}

func TestExecuteTask_AutoCollectErrorDropsFollowUp(t *testing.T) {
	const id = "test_cron_rerun_err"
	prev := autoCollectFn
	t.Cleanup(func() {
		autoCollectFn = prev
		runningCronTasks.Delete(id)
		cronCollectRerun.Delete(id)
	})
	var calls atomic.Int32
	autoCollectFn = func(string, int) error {
		calls.Add(1)
		noteCronCollectRerun(id)
		return errors.New("读取采集站失败")
	}
	executeTask(model.FilmCollectTask{Id: id, Model: model.TaskModelAutoCollect, State: true, Time: 24}, true)
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
	if _, ok := cronCollectRerun.Load(id); ok {
		t.Fatal("failed collect must drop the follow-up flag")
	}
}
