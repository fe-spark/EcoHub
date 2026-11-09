package spider

import (
	"strings"
	"sync"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
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

func TestReportSourcePanic_RecordsAndNotifies(t *testing.T) {
	mu, got := captureNotify(t)
	reportSourcePanic("测试", model.FilmSource{Id: "s1", Name: "站一"}, nil, "bad")
	mu.Lock()
	defer mu.Unlock()
	if len(*got) != 1 || (*got)[0] != "source:s1|panic: bad" {
		t.Fatalf("notify = %v", *got)
	}
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
	runTaskBody(model.FilmCollectTask{Id: "auto", Model: model.TaskModelAutoCollect, State: true, Remark: "自动采集", Time: 24})
	mu.Lock()
	defer mu.Unlock()
	if len(*got) != 1 || !strings.Contains((*got)[0], "auto|自动采集|读取采集站失败") {
		t.Fatalf("notify = %v", *got)
	}
}
