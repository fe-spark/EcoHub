package access

import (
	"sync"
	"sync/atomic"
	"time"

	"server/internal/infra/syslog"
)

const (
	channelSize         = 1024
	workerBatchMaxSize  = 64
	workerBatchMaxWait  = 50 * time.Millisecond
)

var (
	collectorMu     sync.RWMutex
	eventCh         chan *AccessEvent
	workerDone      chan struct{}
	droppedUnsynced int64
	started         atomic.Bool
)

func StartCollector() {
	startDailyRollup()
	if !started.CompareAndSwap(false, true) {
		return
	}
	ch := make(chan *AccessEvent, channelSize)
	done := make(chan struct{})
	collectorMu.Lock()
	eventCh = ch
	workerDone = done
	collectorMu.Unlock()
	go worker(ch, done)
	syslog.Infof("[Access] 采集服务已启动 (enabled=%v)", IsEnabled())
}

// Shutdown 停止接收新埋点事件并等待已缓冲事件在超时窗口内写完。
func Shutdown(timeout time.Duration) {
	collectorMu.Lock()
	ch := eventCh
	done := workerDone
	eventCh = nil
	workerDone = nil
	collectorMu.Unlock()
	started.Store(false)

	if ch == nil {
		return
	}
	close(ch)
	if done == nil {
		return
	}
	if timeout <= 0 {
		<-done
		return
	}
	select {
	case <-done:
	case <-time.After(timeout):
		syslog.Warnf("[Access] 退出排水超时（>%s），部分队列事件未完成写入", timeout)
	}
}

func Collect(evt *AccessEvent) {
	if !IsEnabled() || evt == nil {
		return
	}
	collectorMu.RLock()
	ch := eventCh
	if ch == nil {
		collectorMu.RUnlock()
		return
	}
	select {
	case ch <- evt:
	default:
		atomic.AddInt64(&droppedUnsynced, 1)
	}
	collectorMu.RUnlock()
}

func takeUnsyncedDropped() int64 {
	return atomic.SwapInt64(&droppedUnsynced, 0)
}

func restoreUnsyncedDropped(n int64) {
	if n > 0 {
		atomic.AddInt64(&droppedUnsynced, n)
	}
}

func worker(ch <-chan *AccessEvent, done chan<- struct{}) {
	defer close(done)
	batch := make([]*AccessEvent, 0, workerBatchMaxSize)
	timer := time.NewTimer(workerBatchMaxWait)
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	defer timer.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		func(items []*AccessEvent) {
			defer func() {
				if rec := recover(); rec != nil {
					syslog.Errorf("[Access] worker panic: %v", rec)
				}
			}()
			writeEventsBatch(items)
		}(batch)
		batch = batch[:0]
	}

	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				flush()
				return
			}
			batch = append(batch, evt)
			if len(batch) == 1 {
				timer.Reset(workerBatchMaxWait)
			}
			if len(batch) >= workerBatchMaxSize {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				flush()
			}
		case <-timer.C:
			flush()
		}
	}
}
