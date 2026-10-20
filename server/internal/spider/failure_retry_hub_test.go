package spider

import (
	"context"
	"testing"
	"time"
)

func TestFailureRetryHub_OnlySelectedIDs(t *testing.T) {
	old := failureRetries
	failureRetries = newFailureRetryHub()
	t.Cleanup(func() { failureRetries = old })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := SubscribeFailureRetry(ctx, []uint{1, 3})
	beginFailureRetry(2)
	beginFailureRetry(1)
	select {
	case batch := <-events:
		if len(batch) != 1 || batch[0].ID != 1 || !batch[0].Retrying {
			t.Fatalf("batch = %+v", batch)
		}
	case <-time.After(time.Second):
		t.Fatal("未收到当前页记录的重试开始")
	}

	snap := FailureRetrySnapshot([]uint{1, 3})
	if len(snap) != 1 || snap[0].ID != 1 || !snap[0].Retrying {
		t.Fatalf("snapshot = %+v", snap)
	}
	if len(FailureRetrySnapshot([]uint{2})) != 1 {
		t.Fatal("其它页的在途重试必须仍能被该页快照读到")
	}

	settleFailureRetry(1, true, 0, 1, "", true)
	select {
	case batch := <-events:
		if len(batch) != 1 || !batch[0].Removed || batch[0].Retrying {
			t.Fatalf("settle = %+v", batch)
		}
	case <-time.After(time.Second):
		t.Fatal("未收到重试结果")
	}
	if len(FailureRetrySnapshot([]uint{1})) != 0 {
		t.Fatal("结束后仍在重试快照里")
	}
}

func TestFailureRetryHub_RejectsDuplicate(t *testing.T) {
	old := failureRetries
	failureRetries = newFailureRetryHub()
	t.Cleanup(func() { failureRetries = old })

	if !TryBeginFailureRetry(7) || TryBeginFailureRetry(7) {
		t.Fatal("重复入队必须拒绝")
	}
	snap := FailureRetrySnapshot([]uint{7, 8})
	if len(snap) != 1 || snap[0].ID != 7 || !snap[0].Retrying {
		t.Fatalf("snapshot = %+v", snap)
	}
	abandonFailureRetry(7)
	if len(FailureRetrySnapshot([]uint{7})) != 0 {
		t.Fatal("离开队列后仍在快照里")
	}
}
