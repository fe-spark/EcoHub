package service

import (
	"testing"
	"time"
)

// 后台编排协程 panic 不能带崩进程，panic 之前的工作照常执行。
func TestGoSafe_RecoversPanic(t *testing.T) {
	ran := make(chan struct{})
	goSafe("测试", func() {
		close(ran)
		panic("boom")
	})
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("fn not run")
	}
	time.Sleep(20 * time.Millisecond)
}
