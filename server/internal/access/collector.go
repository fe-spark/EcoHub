package access

import (
	"sync/atomic"

	"server/internal/infra/syslog"
)

const channelSize = 1024

var (
	eventCh         chan *AccessEvent
	droppedTotal    int64
	droppedUnsynced int64
	started         atomic.Bool
)

func StartCollector() {
	startDailyRollup()
	if !started.CompareAndSwap(false, true) {
		return
	}
	eventCh = make(chan *AccessEvent, channelSize)
	go worker()
	syslog.Infof("[Access] 采集服务已启动 (enabled=%v)", IsEnabled())
}

func Collect(evt *AccessEvent) {
	if !IsEnabled() || evt == nil || eventCh == nil {
		return
	}
	select {
	case eventCh <- evt:
	default:
		atomic.AddInt64(&droppedTotal, 1)
		atomic.AddInt64(&droppedUnsynced, 1)
	}
}

func Dropped() int64 {
	return atomic.LoadInt64(&droppedTotal)
}

func takeUnsyncedDropped() int64 {
	return atomic.SwapInt64(&droppedUnsynced, 0)
}

func restoreUnsyncedDropped(n int64) {
	if n > 0 {
		atomic.AddInt64(&droppedUnsynced, n)
	}
}

func worker() {
	for evt := range eventCh {
		func(e *AccessEvent) {
			defer func() {
				if rec := recover(); rec != nil {
					syslog.Errorf("[Access] worker panic: %v", rec)
				}
			}()
			writeEvent(e)
		}(evt)
	}
}
