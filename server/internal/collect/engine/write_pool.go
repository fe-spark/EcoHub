package engine

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/model"
)

const singlePageWriteTimeout = 30 * time.Second

// WriteResult 单页落库执行结果。
type WriteResult struct {
	Page         int
	Stage        string
	AffectedMIDs []int64
	Unmapped     int
	Err          error
}

// WritePool 全局有界写库槽位池，不再按采集站串行阻塞写入。
type WritePool struct {
	gate     *concurrencyGate
	activeWG sync.WaitGroup
}

// NewWritePool 创建有界全局写池。
func NewWritePool(limitFn func() int) *WritePool {
	if limitFn == nil {
		limitFn = config.WriteSlotMax
	}
	return &WritePool{gate: newConcurrencyGate(limitFn)}
}

var defaultWritePool = NewWritePool(nil)

// DefaultWritePool 返回全局默认写池。
func DefaultWritePool() *WritePool {
	return defaultWritePool
}

// WaitPendingWrites 等待全部已开始提交的落库操作完成或超时退出。
func WaitPendingWrites(ctx context.Context) error {
	return defaultWritePool.Wait(ctx)
}

// Submit 异步提交一页落库任务，在槽位可用前受 ctx 取消控制，开始写库后持有 30s 事务超时并捕获 panic。
func (p *WritePool) Submit(
	ctx context.Context,
	page int,
	fn func(context.Context) (model.Mids, error),
	onDone func(WriteResult),
) {
	p.activeWG.Add(1)
	go func() {
		defer p.activeWG.Done()
		if err := p.gate.acquire(ctx); err != nil {
			onDone(WriteResult{Page: page, Stage: "enqueue", Err: err})
			return
		}
		defer p.gate.release()

		// 拿到槽位后再确认一次取消，避免用户点击停止后仍新开写事务。
		if err := ctx.Err(); err != nil {
			onDone(WriteResult{Page: page, Stage: "enqueue", Err: err})
			return
		}

		res := p.runProtectedWrite(page, fn)
		onDone(res)
	}()
}

func (p *WritePool) runProtectedWrite(page int, fn func(context.Context) (model.Mids, error)) (res WriteResult) {
	res = WriteResult{Page: page, Stage: "save"}
	defer func() {
		if r := recover(); r != nil {
			res.Err = fmt.Errorf("写库任务 panic: %v\n%s", r, string(debug.Stack()))
		}
	}()
	writeCtx, cancel := context.WithTimeout(context.Background(), singlePageWriteTimeout)
	defer cancel()

	mids, err := fn(writeCtx)
	res.AffectedMIDs = mids.Affected
	res.Unmapped = mids.Unmapped
	res.Err = err
	return res
}

// Wait 阻塞直到在途写库任务全部结束或上下文取消。
func (p *WritePool) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		p.activeWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
