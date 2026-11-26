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
	tasks        sync.Map
	taskMu       sync.Mutex
	probeCancels = make(map[string]map[uint64]context.CancelFunc)
	probeSeq     uint64
)

// ErrDispatchStopped 任务在登记前被「一键终止」拦截。
var ErrDispatchStopped = errors.New("采集派发已终止")

// ErrTaskExists 同名站点已有活跃任务。
var ErrTaskExists = errors.New("站点已有活跃任务")

// ErrStopRequested 停止已生效，不再登记新的运行。
var ErrStopRequested = errors.New("采集已停止")

type task struct {
	cancel context.CancelFunc
	reqID  string
}

// TryRegisterTask 在 canStart() 返回 nil 且无同名活跃任务时登记任务，返回其取消上下文。
// canStart 在任务表互斥区内求值，用于与「一键终止」、单站停止后的新采集保持互斥；它返回的错误原样带回。
func TryRegisterTask(id, reqID string, canStart func() error) (context.Context, error) {
	taskMu.Lock()
	defer taskMu.Unlock()

	if canStart != nil {
		if err := canStart(); err != nil {
			return nil, err
		}
	}
	if _, ok := tasks.Load(id); ok {
		return nil, ErrTaskExists
	}
	if snap, ok := Snapshot(id); ok && (snap.Status == StatusStopped || snap.Status == StatusStopping) {
		return nil, ErrStopRequested
	}
	ctx, cancel := context.WithCancel(context.Background())
	tasks.Store(id, task{cancel: cancel, reqID: reqID})
	return ctx, nil
}

// RequestStop 停止该站。已登记的运行改为「停止中」并立刻摘出任务表，同时取消在途探页，收尾由原协程完成。
// 还没登记的排队站直接标成已停止。
func RequestStop(id string) {
	taskMu.Lock()
	var taskCancel context.CancelFunc
	if val, ok := tasks.Load(id); ok {
		// 先标成停止中，再取消。避免正在退出的拉页把状态直接写成已停止。
		setStatusIf(id, StatusStopping, StatusStarting, StatusRunning, StatusPaused)
		taskCancel = val.(task).cancel
		tasks.Delete(id)
	} else {
		setStatusIf(id, StatusStopped, StatusStarting, StatusRunning, StatusPaused, StatusStopping)
	}
	cancels := takeProbeCancelsLocked(id)
	taskMu.Unlock()

	if taskCancel != nil {
		taskCancel()
	}
	for _, cancel := range cancels {
		cancel()
	}
}

// WithStopCancel 创建受站点停止信号即时取消的子上下文（用于尚未登记正式任务的探页阶段）。
// 若调用时站点已处于停止或停止中，立即返回已取消的上下文。
func WithStopCancel(parent context.Context, id string) (context.Context, func()) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)

	taskMu.Lock()
	if snap, ok := Snapshot(id); ok && (snap.Status == StatusStopped || snap.Status == StatusStopping) {
		taskMu.Unlock()
		cancel()
		return ctx, func() {}
	}
	probeSeq++
	token := probeSeq
	m := probeCancels[id]
	if m == nil {
		m = make(map[uint64]context.CancelFunc)
		probeCancels[id] = m
	}
	m[token] = cancel
	taskMu.Unlock()

	return ctx, func() {
		cancel()
		taskMu.Lock()
		if m := probeCancels[id]; m != nil {
			delete(m, token)
			if len(m) == 0 {
				delete(probeCancels, id)
			}
		}
		taskMu.Unlock()
	}
}

func takeProbeCancelsLocked(id string) []context.CancelFunc {
	m := probeCancels[id]
	if len(m) == 0 {
		return nil
	}
	cancels := make([]context.CancelFunc, 0, len(m))
	for _, cancel := range m {
		cancels = append(cancels, cancel)
	}
	delete(probeCancels, id)
	return cancels
}

func takeAllProbeCancelsLocked() []context.CancelFunc {
	if len(probeCancels) == 0 {
		return nil
	}
	cancels := make([]context.CancelFunc, 0, len(probeCancels))
	for _, m := range probeCancels {
		for _, cancel := range m {
			cancels = append(cancels, cancel)
		}
	}
	probeCancels = make(map[string]map[uint64]context.CancelFunc)
	return cancels
}

func setStatusIf(id, next string, from ...string) {
	val, ok := store.Load(id)
	if !ok {
		return
	}
	state := val.(*progressState)
	state.mu.Lock()
	defer state.mu.Unlock()
	for _, status := range from {
		if state.data.Status != status {
			continue
		}
		state.data.Status = next
		state.updated = time.Now()
		NotifyChange()
		return
	}
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

// RangeTasks 遍历活跃任务，fn 返回 false 时终止。
func RangeTasks(fn func(id, reqID string) bool) {
	tasks.Range(func(key, value any) bool {
		return fn(key.(string), value.(task).reqID)
	})
}

// MarkAllRunningStopped 把所有 starting/running 的进度置为 stopped，并立即取消在途探页。
func MarkAllRunningStopped() {
	taskMu.Lock()
	// 先标停止再放锁，避免探页在放锁后、改状态前登记而漏取消。
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
	cancels := takeAllProbeCancelsLocked()
	taskMu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
	NotifyChange()
}
