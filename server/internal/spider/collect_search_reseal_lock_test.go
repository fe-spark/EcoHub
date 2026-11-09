package spider

import (
	"testing"
	"time"

	filmsnapshot "server/internal/repository/film/snapshot"
)

// 重建收尾持有重建锁通知监听，监听会拿 q.mu。封口队列等重建时不能反过来拿锁。
func TestResealQueue_NoDeadlockWithRebuildNotify(t *testing.T) {
	prevWait := waitCollectIdle
	prevApply := applySearchReseal
	t.Cleanup(func() {
		waitCollectIdle = prevWait
		applySearchReseal = prevApply
		resetSearchReseal()
		// 全局进度回到空闲，别把 indexing 留给后面的测试。
		filmsnapshot.NotifySearchIndexProgress(0, 0, "idle")
	})
	waitCollectIdle = func() {}
	applySearchReseal = func([]int64) bool {
		// 模拟增量失败转全量重建，让 run 走等重建的分支。
		filmsnapshot.NotifySearchIndexProgress(1, 0, "indexing")
		return false
	}

	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			select {
			case <-stop:
				return
			default:
			}
			// 和 notifySearchIndexIfCurrent 一样：持有重建锁发终态。
			filmsnapshot.WithSearchRebuildScheduled(func(bool) {
				filmsnapshot.NotifySearchIndexProgress(1, 1, "done")
			})
		}
	}()

	// 死锁时主协程会卡在 scheduleSearchReseal 拿 q.mu 上，所以放到子协程里跑，主协程限时等。
	rounds := make(chan struct{})
	go func() {
		defer close(rounds)
		for i := 0; i < 500; i++ {
			scheduleSearchReseal([]int64{int64(i + 1)})
			for {
				searchReseal.mu.Lock()
				running := searchReseal.running
				searchReseal.mu.Unlock()
				if !running {
					break
				}
				time.Sleep(time.Microsecond)
			}
		}
	}()
	select {
	case <-rounds:
	case <-time.After(10 * time.Second):
		// 死锁时 Cleanup 也拿不到 q.mu，直接 panic 打出各协程栈。
		panic("封口队列和重建通知互相等锁")
	}
	close(stop)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("重建通知卡在封口队列锁上")
	}
}
