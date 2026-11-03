package spider

import (
	"testing"

	"server/internal/model"
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

func TestBatchCollectStatus(t *testing.T) {
	// 1. 全部成功：所有站点完成，无失败页
	if s := batchCollectStatus(1, 0, 0, 0, 1, 22, 0); s != model.CollectRecordStatusSuccess {
		t.Fatalf("expected success, got %s", s)
	}
	if s := batchCollectStatus(4, 0, 0, 0, 4, 100, 0); s != model.CollectRecordStatusSuccess {
		t.Fatalf("expected success, got %s", s)
	}

	// 2. 单站部分成功（Image #14 场景：22页成21页，失败1页）：必须判定为 partial，不能是 failed！
	if s := batchCollectStatus(0, 1, 0, 0, 1, 21, 1); s != model.CollectRecordStatusPartial {
		t.Fatalf("expected partial for single source with partial success, got %s", s)
	}

	// 3. 多站部分成功：有站点全成，有站点失败
	if s := batchCollectStatus(3, 0, 1, 0, 4, 86, 1); s != model.CollectRecordStatusPartial {
		t.Fatalf("expected partial for multi source with partial failure, got %s", s)
	}

	// 4. 彻底失败：零页成功，零站成功
	if s := batchCollectStatus(0, 0, 1, 0, 1, 0, 1); s != model.CollectRecordStatusFailed {
		t.Fatalf("expected failed for zero success, got %s", s)
	}
	if s := batchCollectStatus(0, 0, 2, 0, 2, 0, 5); s != model.CollectRecordStatusFailed {
		t.Fatalf("expected failed for zero success multi-source, got %s", s)
	}

	// 5. 用户停止：无失败但有停止
	if s := batchCollectStatus(1, 0, 0, 1, 2, 10, 0); s != model.CollectRecordStatusStopped {
		t.Fatalf("expected stopped, got %s", s)
	}

	// 6. 用户停止但停止前已有失败页：仍是 stopped，不能被失败页翻成 partial
	if s := batchCollectStatus(1, 0, 0, 1, 2, 10, 2); s != model.CollectRecordStatusStopped {
		t.Fatalf("expected stopped even with failed pages before stop, got %s", s)
	}

	// 7. 停止与失败站点并存：有成功产出，定为 partial
	if s := batchCollectStatus(1, 0, 1, 1, 3, 10, 2); s != model.CollectRecordStatusPartial {
		t.Fatalf("expected partial when stopped alongside a failed source, got %s", s)
	}
}

