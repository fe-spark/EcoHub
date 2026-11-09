package spider

import (
	"fmt"
	"runtime/debug"
	"strings"

	"server/internal/infra/syslog"
	"server/internal/model"
)

// reportCronPanic 记录定时任务 panic 的堆栈并发送失败告警。必须在 defer 的 recover 之后调用。
func reportCronPanic(ft model.FilmCollectTask, r any) {
	remark := strings.TrimSpace(ft.Remark)
	if remark == "" {
		remark = ft.Id
	}
	syslog.Errorf("[Spider] 定时任务 panic: task=%s err=%v\n%s", ft.Id, r, debug.Stack())
	emitCronFailedNotify(ft.Id, remark, fmt.Sprintf("panic: %v", r))
}

// withCronRecover 执行 fn 并兜住 panic：打印堆栈并发送定时任务失败告警。
func withCronRecover(ft model.FilmCollectTask, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			reportCronPanic(ft, r)
		}
	}()
	fn()
}

// runCronTaskSafely 执行定时任务并兜住 panic，避免一条任务把整个进程带崩。
func runCronTaskSafely(ft model.FilmCollectTask) {
	withCronRecover(ft, func() { executeTask(ft) })
}

// reportSourcePanic 记录单站采集协程 panic，标记该站失败并发送采集站失败通知。
func reportSourcePanic(tag string, fs model.FilmSource, batchCtx *collectBatchContext, r any) {
	reason := fmt.Sprintf("panic: %v", r)
	syslog.Errorf("[%s] 站点 %s 采集 panic: %v\n%s", tag, fs.Name, r, debug.Stack())
	if batchCtx != nil {
		batchCtx.RecordSourceError(fs.Id, reason)
	}
	emitSourceFailedNotify(fs.Id, fs.Name, reason)
}
