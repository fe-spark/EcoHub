package service

import (
	"runtime/debug"

	"server/internal/infra/syslog"
)

// goSafe 启动后台协程并兜住 panic：只写 syslog 和堆栈，不让一次编排异常把进程带崩。
// 单站采集协程自己已有兜底，这里兜的是派发前后的编排代码。
func goSafe(tag string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				syslog.Errorf("[%s] 后台任务 panic: %v\n%s", tag, r, debug.Stack())
			}
		}()
		fn()
	}()
}
