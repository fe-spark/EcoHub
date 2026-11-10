package spider

import (
	"sync"
	"time"
)

// backgroundCollects 跟踪异步启动的采集批次（例如失败重试），便于测试和退出时等待它们结束。
var backgroundCollects sync.WaitGroup

// goBackgroundCollect 异步启动一批采集并登记到 backgroundCollects。
func goBackgroundCollect(fn func()) {
	backgroundCollects.Add(1)
	go func() {
		defer backgroundCollects.Done()
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
