package spider

import (
	"testing"

	"server/internal/spider/progress"
)

func TestSettleSourceStatus(t *testing.T) {
	status, errMsg := settleSourceStatus(progress.StatusStopping, "context canceled")
	if status != progress.StatusStopped || errMsg != "" {
		t.Fatalf("stopping should settle stopped without error, got %s %q", status, errMsg)
	}

	status, errMsg = settleSourceStatus(progress.StatusStopped, "用户停止")
	if status != progress.StatusStopped || errMsg != "" {
		t.Fatalf("stopped should clear error, got %s %q", status, errMsg)
	}

	status, errMsg = settleSourceStatus(progress.StatusDone, "连接超时")
	if status != progress.StatusFailed || errMsg != "连接超时" {
		t.Fatalf("error should settle failed, got %s %q", status, errMsg)
	}

	status, errMsg = settleSourceStatus(progress.StatusDone, "")
	if status != progress.StatusDone || errMsg != "" {
		t.Fatalf("done without error should stay done, got %s %q", status, errMsg)
	}
}
