package spider

import (
	"strings"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
)

func withRetryTiming(t *testing.T, poll, maxWait time.Duration) {
	t.Helper()
	prevPoll, prevWait := maintenanceRetryPoll, maintenanceRetryMaxWait
	maintenanceRetryPoll, maintenanceRetryMaxWait = poll, maxWait
	prevDB := db.Mdb
	db.Mdb = nil // 补跑时读不到配置，沿用入队时的任务；片库清理在无 DB 时直接返回
	t.Cleanup(func() {
		maintenanceRetryPoll, maintenanceRetryMaxWait = prevPoll, prevWait
		db.Mdb = prevDB
	})
}

func waitDeferredDone(t *testing.T, id string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for isMaintenanceDeferred(id) {
		if time.Now().After(deadline) {
			t.Fatalf("deferred maintenance %s still pending", id)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDeferMaintenance_RunsAfterGateFreesAndDedupes(t *testing.T) {
	withRetryTiming(t, 10*time.Millisecond, 5*time.Second)
	prevExit := maintenanceExitWait
	maintenanceExitWait = 300 * time.Millisecond
	t.Cleanup(func() { maintenanceExitWait = prevExit })

	id := "test_deferred_orphan"
	taskRanAt.Delete(id)
	if err := collectLifecycle.beginMaintenance(); err != nil {
		t.Fatal(err)
	}
	ft := model.FilmCollectTask{Id: id, Model: model.TaskModelOrphanClean, State: true, Remark: "补跑测试"}
	deferMaintenance(ft)
	deferMaintenance(ft) // 去重
	if !isMaintenanceDeferred(id) {
		t.Fatal("expected deferred entry")
	}
	time.Sleep(50 * time.Millisecond)
	if !TaskPrev(id).IsZero() {
		t.Fatal("must not run while gate busy")
	}
	collectLifecycle.endDrain()
	waitDeferredDone(t, id, 3*time.Second)
	if TaskPrev(id).IsZero() {
		t.Fatal("deferred maintenance did not run after gate freed")
	}
}

func TestDeferMaintenance_TimeoutNotifies(t *testing.T) {
	withRetryTiming(t, 5*time.Millisecond, 40*time.Millisecond)
	mu, got := captureNotify(t)
	if err := collectLifecycle.beginMaintenance(); err != nil {
		t.Fatal(err)
	}
	defer collectLifecycle.endDrain()

	id := "test_deferred_timeout"
	deferMaintenance(model.FilmCollectTask{Id: id, Model: model.TaskModelOrphanClean, State: true, Remark: "超时测试"})
	waitDeferredDone(t, id, 2*time.Second)
	mu.Lock()
	defer mu.Unlock()
	if len(*got) != 1 || !strings.Contains((*got)[0], id+"|超时测试|等待其他维护任务结束超过") {
		t.Fatalf("notify = %v", *got)
	}
}

func TestDeferMaintenance_IgnoresLightTasks(t *testing.T) {
	deferMaintenance(model.FilmCollectTask{Id: "light", Model: model.TaskModelLogClean, State: true})
	if isMaintenanceDeferred("light") {
		t.Fatal("light tasks are never deferred")
	}
}
