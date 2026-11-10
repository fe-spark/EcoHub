package spider

import (
	"runtime/debug"
	"sync"
	"time"

	"server/internal/infra/syslog"
)

// backgroundCollects 跟踪异步启动的采集批次（例如失败重试），便于测试和退出时等待它们结束。
var backgroundCollects sync.WaitGroup

// goBackgroundCollect 异步启动一批采集并登记到 backgroundCollects。编排代码 panic 时只记日志，不带崩进程。
func goBackgroundCollect(fn func()) {
	backgroundCollects.Add(1)
	go func() {
		defer backgroundCollects.Done()
		defer func() {
			if r := recover(); r != nil {
				syslog.Errorf("[Spider] 后台采集批次 panic: %v\n%s", r, debug.Stack())
			}
		}()
		fn()
	}()
}

// WaitBackgroundCollects 等待异步启动的采集批次全部结束。超时返回 false。
func WaitBackgroundCollects(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		backgroundCollects.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
