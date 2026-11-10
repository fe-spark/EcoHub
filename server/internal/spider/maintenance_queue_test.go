package spider

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// queueRecorder 替换队列执行：记录执行顺序，可让指定任务阻塞或 panic。
type queueRecorder struct {
	mu      sync.Mutex
	order   []string
	block   map[string]chan struct{}
	started map[string]chan struct{}
	panics  map[string]bool
	states  map[string]bool
}

func installQueueRecorder(t *testing.T, blockIDs ...string) *queueRecorder {
	t.Helper()
	waitMaintenanceQueueIdle()
	r := &queueRecorder{
		block:   map[string]chan struct{}{},
		started: map[string]chan struct{}{},
		panics:  map[string]bool{},
		states:  map[string]bool{},
	}
	for _, id := range blockIDs {
		r.block[id] = make(chan struct{})
		r.started[id] = make(chan struct{})
	}
	maintenanceRunHook = func(ft model.FilmCollectTask) {
		r.mu.Lock()
		r.order = append(r.order, ft.Id)
		r.states[ft.Id] = ft.State
		block, started, doPanic := r.block[ft.Id], r.started[ft.Id], r.panics[ft.Id]
		r.mu.Unlock()
		if started != nil {
			close(started)
		}
		if block != nil {
			<-block
		}
		if doPanic {
			panic("queue boom " + ft.Id)
		}
	}
	t.Cleanup(func() {
		for _, ch := range r.block {
			select {
			case <-ch:
			default:
				close(ch)
			}
		}
		waitMaintenanceQueueIdle()
		maintenanceRunHook = nil
	})
	return r
}

func (r *queueRecorder) waitStarted(t *testing.T, id string) {
	t.Helper()
	select {
	case <-r.started[id]:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not start", id)
	}
}

func (r *queueRecorder) Order() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.order, ",")
}

func mt(id string, m int) model.FilmCollectTask {
	return model.FilmCollectTask{Id: id, Model: m, State: true, Remark: id}
}

func noDB(t *testing.T) {
	t.Helper()
	prev := db.Mdb
	db.Mdb = nil
	t.Cleanup(func() { db.Mdb = prev })
}

func TestPlanCronBatch_MaintenanceWinsCollectSkipped(t *testing.T) {
	collect := mt("collect", model.TaskModelAutoCollect)
	orphan := mt("orphan", model.TaskModelOrphanClean)
	logClean := mt("log", model.TaskModelLogClean)
	record := mt("record", model.TaskModelCollectClean)

	collects, maint, skipped := planCronBatch([]model.FilmCollectTask{collect, orphan, logClean, record, collect}, false)
	if len(collects) != 0 || fmt.Sprint(skipped) != "[collect]" {
		t.Fatalf("collect should be skipped: run=%v skipped=%v", collects, skipped)
	}
	if len(maint) != 3 || maint[0].Id != "orphan" || maint[1].Id != "log" || maint[2].Id != "record" {
		t.Fatalf("maintenance order = %+v", maint)
	}

	// 维护在执行/排队中：单独到点的采集也跳过。
	collects, maint, skipped = planCronBatch([]model.FilmCollectTask{collect}, true)
	if len(collects) != 0 || len(maint) != 0 || fmt.Sprint(skipped) != "[collect]" {
		t.Fatalf("busy: run=%v maint=%v skipped=%v", collects, maint, skipped)
	}

	// 没有维护：采集照常。
	collects, _, skipped = planCronBatch([]model.FilmCollectTask{collect, collect}, false)
	if len(collects) != 1 || len(skipped) != 0 {
		t.Fatalf("collect only: run=%v skipped=%v", collects, skipped)
	}
}

func TestFlushCronQueue_SameSecondCollectSkippedMaintenanceInOrder(t *testing.T) {
	noDB(t)
	r := installQueueRecorder(t, "orphan")
	cronMu.Lock()
	cronPending = []model.FilmCollectTask{
		mt("collect", model.TaskModelAutoCollect),
		mt("orphan", model.TaskModelOrphanClean),
		mt("log", model.TaskModelLogClean),
	}
	cronMu.Unlock()
	flushCronQueue()
	r.waitStarted(t, "orphan")
	if !isMaintenanceRunning() || !IsCronTaskQueued("log") {
		t.Fatal("log clean should wait in queue behind orphan clean")
	}
	if IsCronTaskRunning("collect") {
		t.Fatal("collect must be skipped")
	}
	close(r.block["orphan"])
	waitMaintenanceQueueIdle()
	if got := r.Order(); got != "orphan,log" {
		t.Fatalf("order = %s", got)
	}
}

func TestMaintenanceQueue_QueuedWhileRunningAndDeduped(t *testing.T) {
	noDB(t)
	r := installQueueRecorder(t, "first")
	if !enqueueMaintenance(mt("first", model.TaskModelOrphanClean)) {
		t.Fatal("first enqueue")
	}
	r.waitStarted(t, "first")
	if !enqueueMaintenance(mt("second", model.TaskModelCollectClean)) {
		t.Fatal("second enqueue")
	}
	if enqueueMaintenance(mt("second", model.TaskModelCollectClean)) {
		t.Fatal("duplicate queued task must be deduped")
	}
	if enqueueMaintenance(mt("first", model.TaskModelOrphanClean)) {
		t.Fatal("currently running task must be deduped")
	}
	if !IsCronTaskQueued("second") {
		t.Fatal("second should be queued")
	}
	// 维护排队中：采集到点跳过，手动采集被拒绝。
	if _, _, skipped := planCronBatch([]model.FilmCollectTask{mt("collect", model.TaskModelAutoCollect)}, isMaintenanceRunning()); len(skipped) != 1 {
		t.Fatal("collect must be skipped while maintenance queued")
	}
	if err := rejectManualRun(mt("collect", model.TaskModelAutoCollect)); !errors.Is(err, errCollectDuringMaintenance) {
		t.Fatalf("manual collect = %v", err)
	}
	if err := rejectManualRun(mt("x", model.TaskModelLogClean)); err != nil {
		t.Fatalf("manual maintenance must not be rejected: %v", err)
	}
	close(r.block["first"])
	waitMaintenanceQueueIdle()
	if got := r.Order(); got != "first,second" {
		t.Fatalf("order = %s", got)
	}
	if isMaintenanceRunning() {
		t.Fatal("queue should be idle")
	}
}

func TestMaintenanceQueue_PanicDoesNotStopQueueAndAlerts(t *testing.T) {
	noDB(t)
	mu, got := captureNotify(t)
	r := installQueueRecorder(t)
	r.panics["boom"] = true
	enqueueMaintenance(mt("boom", model.TaskModelOrphanClean))
	enqueueMaintenance(mt("after", model.TaskModelLogClean))
	waitMaintenanceQueueIdle()
	if order := r.Order(); order != "boom,after" {
		t.Fatalf("order = %s", order)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*got) != 1 || !strings.Contains((*got)[0], "boom|boom|panic: queue boom boom") {
		t.Fatalf("notify = %v", *got)
	}
}

func TestMaintenanceQueue_SkipsTaskDisabledBeforeRun(t *testing.T) {
	gdb := setupCronTaskDB(t)
	r := installQueueRecorder(t, "first")
	enqueueMaintenance(mt("first", model.TaskModelOrphanClean))
	r.waitStarted(t, "first")
	enqueueMaintenance(mt("disabled_later", model.TaskModelLogClean))
	gdb.Model(&model.CrontabRecord{}).Where("task_id = ?", "disabled_later").Update("state", false)
	close(r.block["first"])
	waitMaintenanceQueueIdle()
	if order := r.Order(); order != "first" {
		t.Fatalf("disabled task should be skipped, order = %s", order)
	}
}

func setupCronTaskDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s_%d?mode=memory&cache=shared", t.Name(), time.Now().UnixNano())), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.CrontabRecord{}); err != nil {
		t.Fatal(err)
	}
	for _, r := range []model.CrontabRecord{
		{TaskId: "first", TaskModel: model.TaskModelOrphanClean, Spec: "0 0 2 * * *", State: true},
		{TaskId: "disabled_later", TaskModel: model.TaskModelLogClean, Spec: "0 0 3 * * *", State: true},
		{TaskId: "manual_maint", TaskModel: model.TaskModelCollectClean, Spec: "0 30 3 * * *", State: false},
		{TaskId: "manual_collect", TaskModel: model.TaskModelAutoCollect, Spec: "0 0 * * * *", State: true},
	} {
		if err := gdb.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })
	return gdb
}

func TestRunTaskOnce_MaintenanceEnqueuedCollectRejected(t *testing.T) {
	setupCronTaskDB(t)
	r := installQueueRecorder(t, "first")

	// 空闲时手动执行维护：直接开始。
	msg, err := RunTaskOnceWithStatus("first")
	if err != nil || msg != RunOnceStarted {
		t.Fatalf("idle manual run = %q, %v", msg, err)
	}
	r.waitStarted(t, "first")

	// 另一条维护在跑：手动执行的维护排队（即使该任务未启用），返回排队提示。
	msg, err = RunTaskOnceWithStatus("manual_maint")
	if err != nil || msg != RunOnceQueued {
		t.Fatalf("manual maintenance = %q, %v", msg, err)
	}
	if !IsCronTaskQueued("manual_maint") {
		t.Fatal("manual maintenance should be queued")
	}
	if _, err := RunTaskOnceWithStatus("manual_maint"); err == nil {
		t.Fatal("duplicate manual run should be rejected")
	}
	// 维护中手动执行采集：仍然拒绝。
	if _, err := RunTaskOnceWithStatus("manual_collect"); !errors.Is(err, errCollectDuringMaintenance) {
		t.Fatalf("manual collect = %v", err)
	}
	close(r.block["first"])
	waitMaintenanceQueueIdle()
	if order := r.Order(); order != "first,manual_maint" {
		t.Fatalf("order = %s", order)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.states["manual_maint"] {
		t.Fatal("manual run must ignore the disabled switch")
	}
}

func TestMaintenanceQueue_RealRunTakesGate(t *testing.T) {
	noDB(t)
	prevExit := maintenanceExitWait
	maintenanceExitWait = 300 * time.Millisecond
	t.Cleanup(func() { maintenanceExitWait = prevExit })
	waitMaintenanceQueueIdle()
	id := "real_orphan_queue"
	taskRanAt.Delete(id)
	enqueueMaintenance(mt(id, model.TaskModelOrphanClean))
	waitMaintenanceQueueIdle()
	if TaskPrev(id).IsZero() {
		t.Fatal("queued orphan clean did not run through the maintenance gate")
	}
	if collectLifecycle.isMaintenanceBusy() {
		t.Fatal("gate must be released")
	}
}

func TestRunMaintenance_WaitsForBriefGateHolder(t *testing.T) {
	if err := collectLifecycle.beginMaintenance(); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		collectLifecycle.endDrain()
	}()
	ran := false
	if err := collectLifecycle.runMaintenance("test", func() error { ran = true; return nil }); err != nil || !ran {
		t.Fatalf("runMaintenance = %v ran=%v", err, ran)
	}
}
