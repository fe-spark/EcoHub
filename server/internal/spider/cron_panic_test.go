package spider

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/spider/progress"
)

func captureNotify(t *testing.T) (*sync.Mutex, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var got []string
	prev := getNotifyDispatcher()
	RegisterNotifyDispatcher(NotifyDispatcher{
		OnCronFailed: func(taskID, remark, reason string) {
			mu.Lock()
			got = append(got, taskID+"|"+remark+"|"+reason)
			mu.Unlock()
		},
		OnSourceFailed: func(sourceID, sourceName, reason string) {
			mu.Lock()
			got = append(got, "source:"+sourceID+"|"+reason)
			mu.Unlock()
		},
	})
	t.Cleanup(func() { RegisterNotifyDispatcher(prev) })
	return &mu, &got
}

func TestWithCronRecover_NotifiesPanic(t *testing.T) {
	mu, got := captureNotify(t)
	ft := model.FilmCollectTask{Id: "t1", Remark: "清理任务"}
	ran := false
	withCronRecover(ft, func() {
		ran = true
		panic("boom")
	})
	if !ran {
		t.Fatal("fn not run")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*got) != 1 || !strings.Contains((*got)[0], "t1|清理任务|panic: boom") {
		t.Fatalf("notify = %v", *got)
	}
}

func TestReportSourcePanic_LogsWithoutNotify(t *testing.T) {
	mu, got := captureNotify(t)
	if reason := reportSourcePanic("测试", model.FilmSource{Id: "s1", Name: "站一"}, nil, "bad"); reason != "panic: bad" {
		t.Fatalf("reason = %q", reason)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*got) != 0 {
		t.Fatalf("reportSourcePanic must not notify, got %v", *got)
	}
}

// 本批次仍拥有进度时，单站协程 panic 标成失败并告警一次。
func TestDispatchPanic_OwnedNotifiesAndFails(t *testing.T) {
	src := model.FilmSource{Id: "panic-owned", Name: "panic-owned"}
	stubPanicProbe(t, nil)
	mu, got := captureNotify(t)

	claim, err := claimCollectSources([]model.FilmSource{src}, "Batch-Collect")
	if err != nil || len(claim.Sources) != 1 {
		t.Fatalf("claim failed: %v", err)
	}
	batch := newCollectBatchContext(model.CollectTriggerManual, "Batch-Collect", claim.Sources, time.Now()).withClaim(claim)
	defer batch.close()
	runSourcesGroupWithLimit(claim.Sources, 24, "Batch-Collect", 1, stopAllVersion.Load(), batch)

	if snap, _ := progress.Snapshot(src.Id); snap.Status != progress.StatusFailed {
		t.Fatalf("status = %q, want failed", snap.Status)
	}
	if isOccupiedCollectSource(src.Id) {
		t.Fatal("panic must release own occupy")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*got) != 1 || (*got)[0] != "source:"+src.Id+"|panic: probe boom" {
		t.Fatalf("notify = %v", *got)
	}
}

// 一键终止并重试后，旧批次的单站协程才 panic：不能告警，也不能改重试的进度、放掉重试的占用。
func TestDispatchPanic_AfterRetryTakeoverDoesNotNotify(t *testing.T) {
	src := model.FilmSource{Id: "panic-taken-over", Name: "panic-taken-over"}
	gate := make(chan struct{})
	entered := stubPanicProbe(t, gate)

	claim, err := claimCollectSources([]model.FilmSource{src}, "Batch-Collect")
	if err != nil || len(claim.Sources) != 1 {
		t.Fatalf("claim failed: %v", err)
	}
	batch := newCollectBatchContext(model.CollectTriggerManual, "Batch-Collect", claim.Sources, time.Now()).withClaim(claim)
	defer batch.close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		runSourcesGroupWithLimit(claim.Sources, 24, "Batch-Collect", 1, stopAllVersion.Load(), batch)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("probe never started")
	}

	StopAllCollect("测试一键终止")
	retried, err := occupyAndMarkCollectSources([]model.FilmSource{src}, "Single-Collect")
	if err != nil || len(retried) != 1 {
		t.Fatalf("retry occupy failed: %v", err)
	}
	defer releaseCollectSourceIDs(src.Id)
	before, _ := progress.Snapshot(src.Id)
	mu, got := captureNotify(t)

	close(gate)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("old batch did not finish")
	}
	after, _ := progress.Snapshot(src.Id)
	if after.Status != progress.StatusStarting || after.QueueId != before.QueueId {
		t.Fatalf("retry progress changed: %+v", after)
	}
	if !isOccupiedCollectSource(src.Id) {
		t.Fatal("old batch must not release the retry occupy")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*got) != 0 {
		t.Fatalf("taken-over panic must not notify, got %v", *got)
	}
	progress.MarkStopped(src.Id)
}

// stubPanicProbe 探页时 panic。gate 不为 nil 时先等测试放行。
func stubPanicProbe(t *testing.T, gate <-chan struct{}) <-chan struct{} {
	t.Helper()
	entered := make(chan struct{})
	var once sync.Once
	prev := fetchProbePageCount
	fetchProbePageCount = func(context.Context, *model.FilmSource, int) (int, error) {
		once.Do(func() { close(entered) })
		if gate != nil {
			<-gate
		}
		panic("probe boom")
	}
	t.Cleanup(func() { fetchProbePageCount = prev })
	return entered
}

func TestFireCronTask_ConfigReadFailureNotifies(t *testing.T) {
	prevDB := db.Mdb
	db.Mdb = nil
	t.Cleanup(func() { db.Mdb = prevDB })
	mu, got := captureNotify(t)
	fireCronTask("sys_cron_orphan_clean")
	mu.Lock()
	defer mu.Unlock()
	if len(*got) != 1 || !strings.Contains((*got)[0], "sys_cron_orphan_clean|sys_cron_orphan_clean|读取定时任务配置失败") {
		t.Fatalf("notify = %v", *got)
	}
}

func TestRunTaskBody_AutoCollectSourceReadFailureNotifies(t *testing.T) {
	prevDB := db.Mdb
	db.Mdb = nil
	t.Cleanup(func() { db.Mdb = prevDB })
	if searchIndexBlocksCollect() || isMaintenanceRunning() {
		t.Skip("collect gated in this environment")
	}
	mu, got := captureNotify(t)
	runTaskBody(model.FilmCollectTask{Id: "auto", Model: model.TaskModelAutoCollect, State: true, Remark: "自动采集", Time: 24}, false)
	mu.Lock()
	defer mu.Unlock()
	if len(*got) != 1 || !strings.Contains((*got)[0], "auto|自动采集|读取采集站失败") {
		t.Fatalf("notify = %v", *got)
	}
}

func TestRunTaskBody_ManualAutoCollectFailureNoNotify(t *testing.T) {
	prevDB := db.Mdb
	db.Mdb = nil
	t.Cleanup(func() { db.Mdb = prevDB })
	if searchIndexBlocksCollect() || isMaintenanceRunning() {
		t.Skip("collect gated in this environment")
	}
	mu, got := captureNotify(t)
	runTaskBody(model.FilmCollectTask{Id: "auto", Model: model.TaskModelAutoCollect, State: true, Remark: "自动采集", Time: 24}, true)
	mu.Lock()
	defer mu.Unlock()
	if len(*got) != 0 {
		t.Fatalf("manual run must not alert, notify = %v", *got)
	}
}

func TestPublishStreamWindowGuarded_PanicReleasesLock(t *testing.T) {
	orig := publishStreamWindowFn
	publishStreamWindowFn = func([]int64) error { panic("publish boom") }
	t.Cleanup(func() { publishStreamWindowFn = orig })
	if err := publishStreamWindowGuarded([]int64{1}, true); err == nil || !strings.Contains(err.Error(), "publish boom") {
		t.Fatalf("err = %v", err)
	}
	if !publishMu.TryLock() {
		t.Fatal("publishMu must be released after panic")
	}
	publishMu.Unlock()
}
