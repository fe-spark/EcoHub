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
// 全部空闲后整库合并封一次，再把等检索的站点收成完成。
type searchResealQueue struct {
	mu      sync.Mutex
	pending map[int64]struct{}
	sealing map[string]struct{}
	running bool
}

var searchReseal = &searchResealQueue{
	pending: map[int64]struct{}{},
	sealing: map[string]struct{}{},
}

var searchSealRunning atomic.Bool

var waitCollectIdle = func() { collectLifecycle.waitUntilIdle() }

var applySearchReseal = filmsnapshot.ResealSearchIndexMids

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
		if len(q.pending) == 0 && len(q.sealing) == 0 {
			q.running = false
			q.mu.Unlock()
			return
		}
		q.mu.Unlock()

		if collectLifecycle.collecting() {
			waitCollectIdle()
			continue
		}

		q.mu.Lock()
		mids := q.takePendingLocked()
		q.mu.Unlock()

		if len(mids) > 0 {
			searchSealRunning.Store(true)
			applySearchReseal(mids)
			searchSealRunning.Store(false)
		}
		// 封口失败会转全量重建，等重建终态由进度监听收尾。
		if filmsnapshot.IsSearchIndexBuilding() || filmsnapshot.SearchRebuildScheduled() {
			q.mu.Lock()
			q.running = false
			q.mu.Unlock()
			return
		}
		q.complete()
		if len(mids) == 0 {
			// 没有编号也要发一次终态，让前端把待检索收成完成。
			filmsnapshot.NotifySearchIndexProgress(0, 0, "done")
		}
	}
}

func (q *searchResealQueue) takePendingLocked() []int64 {
	mids := make([]int64, 0, len(q.pending))
	for mid := range q.pending {
		mids = append(mids, mid)
	}
	q.pending = map[int64]struct{}{}
	return mids
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

// searchIndexBlocksCollect 检索索引正在加载或封口时，不许再开采集。
func searchIndexBlocksCollect() bool {
	return searchSealRunning.Load() || filmsnapshot.IsSearchIndexBuilding()
}
