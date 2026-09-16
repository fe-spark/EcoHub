package progress

import (
	"context"
	"errors"
	"sync"
	"time"
)

// 活跃采集任务注册表：记录每个站点的取消句柄与本次运行的 reqID。
// reqID 用于保证「任务收尾时注销」只作用于自己那次运行，避免并发重启被误删。

var (
	tasks  sync.Map
	taskMu sync.Mutex
)

// ErrDispatchStopped 任务在登记前被「一键终止」拦截。
var ErrDispatchStopped = errors.New("采集派发已终止")

// ErrTaskExists 同名站点已有活跃任务。
var ErrTaskExists = errors.New("站点已有活跃任务")

type task struct {
	cancel context.CancelFunc
	reqID  string
}

// TryRegisterTask 在 canStart() 为真且无同名活跃任务时登记任务，返回其取消上下文。
// canStart 在任务表互斥区内求值，用于与「一键终止」保持互斥。
func TryRegisterTask(id, reqID string, canStart func() bool) (context.Context, error) {
	taskMu.Lock()
	defer taskMu.Unlock()

	if canStart != nil && !canStart() {
		return nil, ErrDispatchStopped
	}
	if _, ok := tasks.Load(id); ok {
		return nil, ErrTaskExists
	}
	ctx, cancel := context.WithCancel(context.Background())
	tasks.Store(id, task{cancel: cancel, reqID: reqID})
	return ctx, nil
}

// UnregisterTask 在 reqID 匹配时注销任务，返回是否发生注销。
func UnregisterTask(id, reqID string) bool {
	taskMu.Lock()
	defer taskMu.Unlock()

	val, ok := tasks.Load(id)
	if !ok || val.(task).reqID != reqID {
		return false
	}
	tasks.Delete(id)
	return true
}

// TaskCount 当前活跃任务数。
func TaskCount() int {
	n := 0
	tasks.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}

// RangeTasks 遍历活跃任务，fn 返回 false 时终止。
func RangeTasks(fn func(id, reqID string) bool) {
	tasks.Range(func(key, value any) bool {
		return fn(key.(string), value.(task).reqID)
	})
}

// CancelTask 取消指定任务的上下文，返回是否存在该任务。
func CancelTask(id string) bool {
	val, ok := tasks.Load(id)
	if !ok {
		return false
	}
	val.(task).cancel()
	return true
}

// CancelAllTasks 取消全部活跃任务并返回取消数量。
func CancelAllTasks() int {
	count := 0
	tasks.Range(func(_, value any) bool {
		value.(task).cancel()
		count++
		return true
	})
	return count
}

// MarkAllRunningStopped 把所有 starting/running 的进度置为 stopped。
func MarkAllRunningStopped() {
	store.Range(func(_, value any) bool {
		state := value.(*progressState)
		state.mu.Lock()
		if state.data.Status == StatusStarting || state.data.Status == StatusRunning {
			state.data.Status = StatusStopped
			state.updated = time.Now()
		}
		state.mu.Unlock()
		return true
	})
}
