package spider

import (
	"context"
	"sync"

	"server/internal/model"
)

var (
	recordCompletionMu          sync.RWMutex
	recordCompletionSubscribers = make(map[chan *model.CollectRecordProgressPayload]struct{})
)

// SubscribeRecordCompletion 订阅批次结算完成通知
func SubscribeRecordCompletion(ctx context.Context) <-chan *model.CollectRecordProgressPayload {
	ch := make(chan *model.CollectRecordProgressPayload, 16)
	recordCompletionMu.Lock()
	recordCompletionSubscribers[ch] = struct{}{}
	recordCompletionMu.Unlock()

	go func() {
		<-ctx.Done()
		recordCompletionMu.Lock()
		delete(recordCompletionSubscribers, ch)
		recordCompletionMu.Unlock()
		close(ch)
	}()
	return ch
}

// BroadcastRecordCompletion 广播批次终态更新事件
func BroadcastRecordCompletion(p *model.CollectRecordProgressPayload) {
	if p == nil {
		return
	}
	recordCompletionMu.RLock()
	defer recordCompletionMu.RUnlock()
	for ch := range recordCompletionSubscribers {
		select {
		case ch <- p:
			default:
		}
	}
}
