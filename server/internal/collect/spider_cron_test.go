package collect

import (
	"context"
	"errors"
	"fmt"
	"sync"
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
	executeLogCleanTask(ft, false)

	// Test with empty remark fallback
	ft2 := model.FilmCollectTask{
		Id:     "test_sys_log_clean_empty_remark",
		Model:  4,
		State:  true,
		Remark: "",
	}
	executeLogCleanTask(ft2, false)
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
	executeOrphanCleanTask(ft, false)

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
	finishMaintenance(model.FilmCollectTask{Id: "sys_cron_orphan_clean"}, false, "片库清理", errors.New("刷新读模型失败"))
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
	finishMaintenance(model.FilmCollectTask{Id: "sys_cron_orphan_clean"}, false, "片库清理", err)
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

func TestExecuteTask_AutoCollectOverlapRunsConcurrentlyWithRefCount(t *testing.T) {
	const id = "test_cron_overlap_concurrent"
	prev := autoCollectFn
	t.Cleanup(func() {
		autoCollectFn = prev
		runningCronMu.Lock()
		delete(runningCronCounts, id)
		runningCronMu.Unlock()
	})

	var calls atomic.Int32
	round1Started := make(chan struct{})
	round1Release := make(chan struct{})
	autoCollectFn = func(string, int) error {
		if calls.Add(1) == 1 {
			close(round1Started)
			<-round1Release
		}
		return nil
	}

	ft := model.FilmCollectTask{Id: id, Model: model.TaskModelAutoCollect, State: true, Time: 24}
	done1 := make(chan struct{})
	go func() {
		executeTask(ft, false)
		close(done1)
	}()
	select {
	case <-round1Started:
	case <-time.After(time.Second):
		t.Fatal("first collect did not start")
	}

	// 第 1 轮尚未结束时触发第 2 轮，不应排队或跳过整个任务，应立即进入第 2 轮（由底层按站跳过忙碌站）。
	executeTask(ft, false)
	if got := calls.Load(); got != 2 {
		t.Fatalf("overlap trigger should enter immediately without queuing rerun, calls=%d", got)
	}
	if !IsCronTaskRunning(id) {
		t.Fatal("round 1 仍在执行时 IsCronTaskRunning 必须保持为 true")
	}

	close(round1Release)
	<-done1
	if IsCronTaskRunning(id) {
		t.Fatal("两轮都结束后 IsCronTaskRunning 应回到 false")
	}
}

func TestExecuteTask_MaintenanceOverlapStillSkips(t *testing.T) {
	const id = "test_maint_overlap_skip"
	enterCronTaskRun(id, true)
	t.Cleanup(func() {
		runningCronMu.Lock()
		delete(runningCronCounts, id)
		runningCronMu.Unlock()
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// 非自动采集任务在执行中时仍应跳过。
		executeTask(model.FilmCollectTask{Id: id, Model: model.TaskModelLogClean, State: true}, false)
	}()
	wg.Wait()
	if !IsCronTaskRunning(id) {
		t.Fatal("被跳过的清理任务不应清掉原任务的运行态")
	}
	if isMaintenanceModel(model.TaskModelAutoCollect) || isMaintenanceModel(99) ||
		!isMaintenanceModel(model.TaskModelOrphanClean) || !isMaintenanceModel(model.TaskModelLogClean) || !isMaintenanceModel(model.TaskModelCollectClean) {
		t.Fatal("除自动采集外，所有受支持任务均应属于维护任务")
	}
}

// 按站点粒度防重：第 1 轮 busy-site 未结束时，新到点直接跳过 busy-site 并让 idle-site 立刻启动。
func TestAutoCollectOverlap_SkipsBusySiteStartsIdleSite(t *testing.T) {
	resetSearchReseal()
	stubStreamPublishForTest(t, func([]int64) error { return nil }, func() string { return "" })
	origProbe := fetchProbePageCount
	origSeal := sealSearchIndex
	t.Cleanup(func() {
		fetchProbePageCount = origProbe
		sealSearchIndex = origSeal
		resetSearchReseal()
	})
	sealSearchIndex = func([]int64) bool { return true }

	gdb := setupAuditSpiderDB(t)
	busySrc := model.FilmSource{Id: "overlap-busy", Name: "忙碌站", Uri: "http://127.0.0.1/busy", State: true, Format: "json"}
	idleSrc := model.FilmSource{Id: "overlap-idle", Name: "空闲站", Uri: "http://127.0.0.1/idle", State: true, Format: "json"}
	if err := gdb.Create([]model.FilmSource{busySrc, idleSrc}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.Category{Id: 10, Name: "电影"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create([]model.CategoryMapping{
		{SourceId: busySrc.Id, SourceTypeId: 1, CategoryId: 10},
		{SourceId: idleSrc.Id, SourceTypeId: 1, CategoryId: 10},
	}).Error; err != nil {
		t.Fatal(err)
	}

	// 模拟第 1 轮正在采集 overlap-busy。
	busyClaim, err := claimCollectSources([]model.FilmSource{busySrc}, model.CollectTagCronCollect)
	if err != nil || len(busyClaim.Sources) != 1 {
		t.Fatalf("occupy busySrc failed: %v", err)
	}
	defer busyClaim.abandon()

	var mu sync.Mutex
	var probed []string
	fetchProbePageCount = func(_ context.Context, s *model.FilmSource, _ int) (int, error) {
		mu.Lock()
		probed = append(probed, s.Id)
		mu.Unlock()
		return 0, nil
	}

	runAutoCollectTask(model.FilmCollectTask{Id: "task_overlap_1", Remark: "按站跳过", Time: 24, State: true}, false)

	mu.Lock()
	defer mu.Unlock()
	if len(probed) != 1 || probed[0] != idleSrc.Id {
		t.Fatalf("应只采集空闲站 [%s] 并跳过忙碌站 [%s]，实际=%v", idleSrc.Id, busySrc.Id, probed)
	}
}

func TestRunTaskOnceWithStatus_AutoCollectOverlapStartsConcurrently(t *testing.T) {
	setupAuditSpiderDB(t)
	prevAuto := autoCollectFn
	const id = "manual_auto_overlap"
	t.Cleanup(func() {
		autoCollectFn = prevAuto
		runningCronMu.Lock()
		delete(runningCronCounts, id)
		runningCronMu.Unlock()
	})

	if err := SaveFilmTask(model.FilmCollectTask{
		Id:    id,
		Spec:  "0 0 * * * *",
		Model: model.TaskModelAutoCollect,
		Time:  24,
		State: true,
	}); err != nil {
		t.Fatal(err)
	}

	firstEntered := make(chan struct{})
	firstRelease := make(chan struct{})
	secondDone := make(chan struct{})
	var calls atomic.Int32
	autoCollectFn = func(string, int) error {
		if calls.Add(1) == 1 {
			close(firstEntered)
			<-firstRelease
			return nil
		}
		close(secondDone)
		return nil
	}

	msg1, err := RunTaskOnceWithStatus(id)
	if err != nil || msg1 != RunOnceStarted {
		t.Fatalf("首次手动触发失败 msg=%q err=%v", msg1, err)
	}
	<-firstEntered

	msg2, err := RunTaskOnceWithStatus(id)
	if err != nil || msg2 != RunOnceStarted {
		t.Fatalf("自动采集重叠手动触发应立即放行，实际 msg=%q err=%v", msg2, err)
	}
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("第 2 次手动触发未并发进入执行")
	}
	if !IsCronTaskRunning(id) {
		t.Fatal("第 1 次尚未结束时 IsCronTaskRunning 应为 true")
	}

	close(firstRelease)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && IsCronTaskRunning(id) {
		time.Sleep(10 * time.Millisecond)
	}
	if IsCronTaskRunning(id) {
		t.Fatal("两次均结束后 IsCronTaskRunning 应归 false")
	}
}
