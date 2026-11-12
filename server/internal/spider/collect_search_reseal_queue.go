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
// 全部采集队列都结束后再封一次。编号是否完整增量交给同一次封口判断。
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

// sealSearchIndex 全部队列结束后的唯一封口。第二个参数为真时编号不是完整增量，函数在点查前整表顺序加载。
var sealSearchIndex = filmsnapshot.ResealSearchIndex

// collectQueueCount 正在跑的采集队列。封口要等每一条队列结束，不能只看当前没有站在抓页。
// frozen 只在本轮已经没有队列、准备落地封口时置位。还有队列时新队列可以加入这一轮。
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
	collectQueues.cond.Broadcast()
	collectQueues.mu.Unlock()
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

// noteSearchResealFullCollect 本轮编号不是完整增量（含全量站）。封口时在点查前整表顺序加载。
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
	defer unfreezeCollectQueues()
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
		if !freezeCollectQueuesIfIdle() {
			waitCollectQueues()
			continue
		}
		if q.sealFrozenRound() {
			return
		}
	}
}

// sealFrozenRound 落地已经冻结的这一轮封口。返回 true 表示调用方应结束封口循环。
func (q *searchResealQueue) sealFrozenRound() bool {
	defer unfreezeCollectQueues()

	q.mu.Lock()
	if len(q.pending) == 0 && len(q.sealing) == 0 && !q.fullCollect {
		q.running = false
		q.mu.Unlock()
		return true
	}
	mids, full := q.takePendingLocked()
	q.mu.Unlock()

	if full || len(mids) > 0 {
		searchSealRunning.Store(true)
		sealSearchIndex(mids, full)
		searchSealRunning.Store(false)
	}
	// 封口函数决定整表加载时会排队重建。已交出的站点等重建结束再收口。
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
		return parked
	}
	// 按编号封口成功时 ResealSearchIndex 已发过 done；这里只收口 sealing，不再依赖全局监听。
	q.complete()
	if len(mids) == 0 && !full {
		// 没有编号也要发一次终态，让前端把待检索收成完成。
		filmsnapshot.NotifySearchIndexProgress(0, 0, "done")
	}
	return false
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
