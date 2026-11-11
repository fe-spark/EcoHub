package spider

import (
	"strings"
	"sync"
	"sync/atomic"

	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider/progress"
)

// searchResealQueue 全局封口周期。
// 采集跑完的站点先进 sealing 等检索，所有站在抓页时只攒编号；
// 全部采集队列都结束后再封一次。任一队列里有全量站就整表重建，否则按编号增量封口。
type searchResealQueue struct {
	mu          sync.Mutex
	pending     map[int64]struct{}
	sealing     map[string]struct{}
	running     bool
	fullCollect bool
	// awaitDone 停等本轮触发的全量重建终态。只有这时才响应 done/idle。
	awaitDone bool
}

var searchReseal = &searchResealQueue{
	pending: map[int64]struct{}{},
	sealing: map[string]struct{}{},
}

var searchSealRunning atomic.Bool

var waitCollectIdle = func() { collectLifecycle.waitUntilIdle() }

var applySearchReseal = filmsnapshot.ResealSearchIndexMids

// requestSearchRebuild 全量站参与本轮时整表重建，不再按编号点查。
var requestSearchRebuild = filmsnapshot.RequestActiveFilmSearchIndexRebuild

// collectQueueCount 正在跑的采集队列。封口要等每一条队列结束，不能只看当前没有站在抓页。
type collectQueueCount struct {
	mu   sync.Mutex
	cond *sync.Cond
	n    int
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

func endCollectQueue() {
	collectQueues.mu.Lock()
	if collectQueues.n > 0 {
		collectQueues.n--
	}
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

func isFullCollectHour(hour int) bool {
	return hour < 0
}

// addSealing 采集跑完后挂进本次封口周期。空封口也要走完，保证每次都有待检索阶段。
func (q *searchResealQueue) addSealing(sourceID string) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	q.mu.Lock()
	q.sealing[sourceID] = struct{}{}
	start := !q.running
	if start {
		q.running = true
	}
	q.mu.Unlock()

	notifySealWaiting()
	if start {
		go q.run()
	}
}

// noteSearchResealFullCollect 本轮任一采集队列里有全量站。封口时整表重建。
func noteSearchResealFullCollect() {
	q := searchReseal
	q.mu.Lock()
	q.fullCollect = true
	start := !q.running
	if start {
		q.running = true
	}
	q.mu.Unlock()

	notifySealWaiting()
	if start {
		go q.run()
	}
}

func scheduleSearchReseal(mids []int64) {
	q := searchReseal
	q.mu.Lock()
	added := 0
	for _, mid := range mids {
		if mid > 0 {
			q.pending[mid] = struct{}{}
			added++
		}
	}
	if added == 0 {
		q.mu.Unlock()
		return
	}
	start := !q.running
	if start {
		q.running = true
	}
	q.mu.Unlock()

	notifySealWaiting()
	if start {
		go q.run()
	}
}

func notifySealWaiting() {
	if searchSealRunning.Load() || filmsnapshot.IsSearchIndexBuilding() {
		return
	}
	filmsnapshot.NotifySearchIndexProgress(0, 0, "waiting")
}

func (q *searchResealQueue) run() {
	for {
		q.mu.Lock()
		if len(q.pending) == 0 && len(q.sealing) == 0 && !q.fullCollect {
			q.running = false
			q.mu.Unlock()
			return
		}
		q.mu.Unlock()

		if collectLifecycle.collecting() {
			waitCollectIdle()
			continue
		}
		if collectQueueActive() {
			waitCollectQueues()
			continue
		}

		q.mu.Lock()
		mids, full := q.takePendingLocked()
		q.mu.Unlock()

		if full {
			searchSealRunning.Store(true)
			requestSearchRebuild()
			searchSealRunning.Store(false)
		} else if len(mids) > 0 {
			searchSealRunning.Store(true)
			applySearchReseal(mids)
			searchSealRunning.Store(false)
		}
		// 增量封口失败会转全量重建。已交出的站点等重建结束再收口。
		// 重建期间新进来的编号还在 pending，继续封。
		if filmsnapshot.IsSearchIndexBuilding() || filmsnapshot.SearchRebuildScheduled() {
			parked := false
			// 先拿重建锁再拿 q.mu，和重建收尾通知监听的顺序一致。
			filmsnapshot.WithSearchRebuildScheduled(func(scheduled bool) {
				q.mu.Lock()
				defer q.mu.Unlock()
				if len(q.pending) > 0 || !(scheduled || filmsnapshot.IsSearchIndexBuilding()) {
					return
				}
				q.running = false
				q.awaitDone = true
				parked = true
			})
			if parked {
				return
			}
			continue
		}
		// 增量成功时 ResealSearchIndexMids 已发过 done；这里只收口 sealing，不再依赖全局监听。
		q.complete()
		if len(mids) == 0 && !full {
			// 没有编号也要发一次终态，让前端把待检索收成完成。
			filmsnapshot.NotifySearchIndexProgress(0, 0, "done")
		}
	}
}

func (q *searchResealQueue) takePendingLocked() ([]int64, bool) {
	mids := make([]int64, 0, len(q.pending))
	for mid := range q.pending {
		mids = append(mids, mid)
	}
	q.pending = map[int64]struct{}{}
	full := q.fullCollect
	q.fullCollect = false
	return mids, full
}

// complete 把当前等检索的站点收成完成。已经重开的站点保持原状。
func (q *searchResealQueue) complete() {
	q.mu.Lock()
	ids := make([]string, 0, len(q.sealing))
	for id := range q.sealing {
		ids = append(ids, id)
	}
	q.sealing = map[string]struct{}{}
	q.mu.Unlock()
	progress.MarkSealed(ids)
}

// onSearchIndexTerminal 仅在停等本轮重建时处理终态：done 收口，idle 表示失败不收口。
func (q *searchResealQueue) onSearchIndexTerminal(status string) {
	q.mu.Lock()
	if !q.awaitDone {
		q.mu.Unlock()
		return
	}
	q.awaitDone = false
	q.mu.Unlock()
	if status == "done" {
		q.complete()
	}
}

// searchIndexBlocksCollect 检索索引正在加载、重建排队或封口队列运行时，不许再开采集。
func searchIndexBlocksCollect() bool {
	if searchSealRunning.Load() || filmsnapshot.IsSearchIndexBuilding() || filmsnapshot.SearchRebuildScheduled() {
		return true
	}
	searchReseal.mu.Lock()
	running := searchReseal.running || searchReseal.awaitDone
	searchReseal.mu.Unlock()
	return running
}
