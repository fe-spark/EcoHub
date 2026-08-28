package access

import (
	"sync/atomic"

	"server/internal/config"
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
	if !config.AccessLogEnabled {
		syslog.Infof("[Access] 访问分析已关闭")
		return
	}
	if !started.CompareAndSwap(false, true) {
		return
	}
	eventCh = make(chan *AccessEvent, channelSize)
	go worker()
	syslog.Infof("[Access] 采集已启动")
}

func Collect(evt *AccessEvent) {
	if evt == nil || eventCh == nil {
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
