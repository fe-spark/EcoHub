package film

import (
	"strings"
	"testing"
	"time"
)

func TestWatchResetProgress(t *testing.T) {
	stage := "watch-" + t.Name()
	got := make(chan ResetProgress, 4)
	unsub := WatchResetProgress(func(p ResetProgress) {
		if p.Stage == stage || strings.Contains(p.Error, stage) {
			got <- p
		}
	})
	t.Cleanup(func() {
		unsub()
		resetProg.mu.Lock()
		resetProg.running = false
		resetProg.percent = 0
		resetProg.stage = ""
		resetProg.errMsg = ""
		resetProg.mu.Unlock()
	})

	ReportResetProgress(40, stage)
	select {
	case p := <-got:
		if p.Percent != 40 || p.Stage != stage {
			t.Fatalf("progress: %+v", p)
		}
	case <-time.After(time.Second):
		t.Fatal("progress event missing")
	}

	unsub()
	ReportResetProgress(50, stage)
	select {
	case p := <-got:
		t.Fatalf("unsubscribed watcher received %+v", p)
	case <-time.After(50 * time.Millisecond):
	}
}
