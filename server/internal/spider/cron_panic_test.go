package spider

import (
	"strings"
	"sync"
	"testing"

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
