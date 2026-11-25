package utils

import (
	"runtime/debug"

	"server/internal/infra/syslog"
)

// GoSafe 启动后台协程并兜住 panic：只写 syslog 和堆栈，不让一次编排异常把进程带崩。
func GoSafe(tag string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				syslog.Errorf("[%s] 后台任务 panic: %v\n%s", tag, r, debug.Stack())
			}
		}()
		fn()
	}()
}
