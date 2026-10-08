package spider

import (
	"testing"
	"time"

	"server/internal/model"
)

func TestBackupCollectIntervalRange(t *testing.T) {
	for _, id := range []string{"a", "src_1", "zzzz", "基准"} {
		got := backupCollectInterval(id)
		if got < 6*time.Hour || got > 12*time.Hour {
			t.Fatalf("id=%s interval=%s out of 6-12h", id, got)
		}
	}
}

func TestCollectDispatchDelay(t *testing.T) {
	manual := collectDispatchDelay(&collectBatchContext{trigger: model.NotifyTriggerManual})
	if manual != 200*time.Millisecond {
		t.Fatalf("manual delay want 200ms, got %s", manual)
	}
	cron := collectDispatchDelay(&collectBatchContext{trigger: model.NotifyTriggerCron})
	if cron < 5*time.Second || cron > 15*time.Second {
		t.Fatalf("cron jitter want 5-15s, got %s", cron)
	}
}
