package collect

import (
	"fmt"
	"log"
	"server/internal/collect/progress"
	filmsnapshot "server/internal/film/snapshot"
	"strings"
	"sync"
	"time"
)

type collectQueueCount struct {
	mu     sync.Mutex
	cond   *sync.Cond
	n      int
	frozen bool
}

func newCollectQueueCount() *collectQueueCount {
	c := &collectQueueCount{}
	c.cond = sync.NewCond(&c.mu)
	return c
}

var collectQueues = newCollectQueueCount()

func beginCollectQueue() {
	collectQueues.mu.Lock()
	collectQueues.n++
	collectQueues.mu.Unlock()
}

// tryBeginCollectQueue 占用一条采集队列。索引正在更新，或封口已经没有队列、准备落地时，拒绝加入。
// 拒绝空档的同时冻结入队，和封口线程看到的空闲是同一次判断。
func tryBeginCollectQueue() (bool, error) {
	if indexBuildBlocksCollect() {
		return false, errSearchIndexUpdating
	}
	collectQueues.mu.Lock()
	defer collectQueues.mu.Unlock()
	if collectQueues.frozen {
		return false, errSearchSealPending
	}
	searchReseal.mu.Lock()
	open := searchReseal.running
	searchReseal.mu.Unlock()
	if open && collectQueues.n == 0 {
		collectQueues.frozen = true
		return false, errSearchSealPending
	}
	collectQueues.n++
	return true, nil
}

func endCollectQueue() {
	collectQueues.mu.Lock()
	if collectQueues.n > 0 {
		collectQueues.n--
	}
	n := collectQueues.n
	collectQueues.cond.Broadcast()
	collectQueues.mu.Unlock()
	log.Printf("[Spider] 采集队列结束 剩余队列=%d", n)
}

// freezeCollectQueuesIfIdle 本轮封口准备落地。还有队列时返回 false，调用方继续等。
func freezeCollectQueuesIfIdle() bool {
	collectQueues.mu.Lock()
	defer collectQueues.mu.Unlock()
	if collectQueues.n > 0 {
		return false
	}
	collectQueues.frozen = true
	return true
}

func unfreezeCollectQueues() {
	collectQueues.mu.Lock()
	collectQueues.frozen = false
	collectQueues.cond.Broadcast()
	collectQueues.mu.Unlock()
}

func collectQueueActive() bool {
	collectQueues.mu.Lock()
	defer collectQueues.mu.Unlock()
	return collectQueues.n > 0
}

func waitCollectQueues() {
	collectQueues.mu.Lock()
	for collectQueues.n > 0 {
		collectQueues.cond.Wait()
	}
	collectQueues.mu.Unlock()
}

func queueCount() (n int, frozen bool) {
	collectQueues.mu.Lock()
	defer collectQueues.mu.Unlock()
	return collectQueues.n, collectQueues.frozen
}

func waitSealSourcesIdle() bool {
	active, _, _ := collectLifecycle.activity()
	if active == 0 {
		return true
	}
	logSealSources("封口等待抓页结束")
	done := make(chan struct{})
	go func() {
		waitCollectIdle()
		close(done)
	}()
	ticker := time.NewTicker(sealWaitLogGap)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			log.Printf("[Spider] 封口抓页已结束")
			return true
		case <-ticker.C:
			logSealSources("封口仍在等抓页")
		}
	}
}

func waitSealQueuesIdle() {
	n, _ := queueCount()
	if n == 0 {
		return
	}
	logSealQueues("封口等待采集队列")
	done := make(chan struct{})
	go func() {
		waitCollectQueues()
		close(done)
	}()
	ticker := time.NewTicker(sealWaitLogGap)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			log.Printf("[Spider] 封口采集队列已空")
			return
		case <-ticker.C:
			logSealQueues("封口仍在等采集队列")
		}
	}
}

func logSealSources(action string) {
	active, publishing, sources := collectLifecycle.activity()
	log.Printf("[Spider] %s active=%d publishing=%d sources=%d %s", action, active, publishing, len(sources), formatSealSources(sources))
}

func logSealQueues(action string) {
	n, frozen := queueCount()
	log.Printf("[Spider] %s 剩余队列=%d frozen=%t", action, n, frozen)
}

func formatSealSources(ids []string) string {
	if len(ids) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		status := "?"
		if snap, ok := progress.Snapshot(id); ok {
			status = snap.Status
		}
		parts = append(parts, id+"="+status)
		if len(parts) >= 8 {
			break
		}
	}
	text := strings.Join(parts, ",")
	if len(ids) > len(parts) {
		text += fmt.Sprintf(" +%d", len(ids)-len(parts))
	}
	return text
}

// addSealing 采集跑完后挂进本次封口周期。空封口也要走完，保证每次都有待检索阶段。
// indexBuildBlocksCollect 索引正在构建、重建已排队，或在等重建终态。
func indexBuildBlocksCollect() bool {
	if searchSealRunning.Load() || filmsnapshot.IsSearchIndexBuilding() || filmsnapshot.SearchRebuildScheduled() {
		return true
	}
	searchReseal.mu.Lock()
	awaitDone := searchReseal.awaitDone
	searchReseal.mu.Unlock()
	return awaitDone
}

// collectAdmissionClosed 封口已冻结，或本轮已打开且当前没有采集队列。
// 还有队列在跑时返回 false：新队列可以加入，封口会继续等。
func collectAdmissionClosed() bool {
	collectQueues.mu.Lock()
	defer collectQueues.mu.Unlock()
	if collectQueues.frozen {
		return true
	}
	if collectQueues.n > 0 {
		return false
	}
	searchReseal.mu.Lock()
	open := searchReseal.running
	searchReseal.mu.Unlock()
	return open
}

// collectPauseErr 现在不能开新的采集队列时返回原因。还有队列在跑时返回 nil。
func collectPauseErr() error {
	if indexBuildBlocksCollect() {
		return errSearchIndexUpdating
	}
	if collectAdmissionClosed() {
		return errSearchSealPending
	}
	return nil
}

func collectPauseReason() string {
	if err := collectPauseErr(); err != nil {
		return err.Error()
	}
	return ""
}

// searchIndexBlocksCollect 索引正在更新，或封口已经进入空档时，不许再开采集。
func searchIndexBlocksCollect() bool {
	return collectPauseErr() != nil
}
