package syslog

import "context"

// Subscribe 订阅后续日志写入。通道容量 1，多次写入合并成一次唤醒。
// ctx 结束时取消订阅并关闭通道。写日志不会被慢订阅者堵住。
func Subscribe(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		ctx = context.Background()
	}
	ch := make(chan struct{}, 1)
	defaultLogger.mu.Lock()
	if defaultLogger.watchers == nil {
		defaultLogger.watchers = map[chan struct{}]struct{}{}
	}
	defaultLogger.watchers[ch] = struct{}{}
	defaultLogger.mu.Unlock()

	go func() {
		<-ctx.Done()
		defaultLogger.mu.Lock()
		delete(defaultLogger.watchers, ch)
		defaultLogger.mu.Unlock()
		close(ch)
	}()
	return ch
}

// notifyLocked 唤醒订阅者。调用方已持有 l.mu。通道满则跳过，数据仍在缓冲里。
func (l *rollingLogger) notifyLocked() {
	for ch := range l.watchers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
