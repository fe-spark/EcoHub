package ingest

import (
	"sync"
	"sync/atomic"
	"time"

	"server/internal/analytics/config"
	"server/internal/analytics/store"
	"server/internal/analytics/types"
	"server/internal/infra/syslog"
)

const (
	channelSize        = 1024
	workerBatchMaxSize = 64
	workerBatchMaxWait = 50 * time.Millisecond
)

var (
	collectorMu sync.RWMutex
	eventCh     chan *types.Event
	workerDone  chan struct{}
	started     atomic.Bool
)

// StartCollector 启动异步埋点批处理协程（幂等）
func StartCollector() {
	if !started.CompareAndSwap(false, true) {
		return
	}
	ch := make(chan *types.Event, channelSize)
	done := make(chan struct{})
	collectorMu.Lock()
	eventCh = ch
	workerDone = done
	collectorMu.Unlock()
	go worker(ch, done)
	syslog.Infof("[Analytics] 采集队列已启动 (enabled=%v)", config.IsEnabled())
}

// Shutdown 停止接收新事件并等待缓冲队列在超时时间内排空落库
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
		syslog.Warnf("[Analytics] 退出排水超时（>%s），部分缓冲事件未完成写入", timeout)
	}
}

// Collect 非阻塞投递埋点事件；队列满时累计 dropped 指标，绝不阻塞业务请求
func Collect(evt *types.Event) {
	if !config.IsEnabled() || evt == nil {
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
		store.AddDropped(1)
	}
	collectorMu.RUnlock()
}

func worker(ch <-chan *types.Event, done chan<- struct{}) {
	defer close(done)
	batch := make([]*types.Event, 0, workerBatchMaxSize)
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
		func(items []*types.Event) {
			defer func() {
				if rec := recover(); rec != nil {
					syslog.Errorf("[Analytics] worker panic: %v", rec)
				}
			}()
			store.WriteBatch(items)
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
