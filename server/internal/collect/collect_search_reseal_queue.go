package collect

import (
	"log"
	"reflect"
	"server/internal/collect/progress"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const sealWaitLogGap = 15 * time.Second

// searchResealQueue 全局封口周期。
// 采集跑完的站点先进 sealing 等检索，所有站在抓页时只攒编号；
// 全部采集队列都结束后再封一次。按编号增量更新内存索引。
type searchResealQueue struct {
	mu                 sync.Mutex
	pending            map[int64]struct{}
	verifiedPrimary    string
	allPendingVerified bool
	sealing            map[string]struct{}
	running            bool
	retryWait          time.Duration
	retryTimer         *time.Timer
	rebuildRetries     int
	sealRetries        int
	// awaitDone 停等本轮触发的整表重建终态。只有这时才响应 done/idle。
	awaitDone bool
}

var searchReseal = &searchResealQueue{
	pending: map[int64]struct{}{},
	sealing: map[string]struct{}{},
}

var searchSealRunning atomic.Bool

var waitCollectIdle = func() { collectLifecycle.waitUntilIdle() }

// sealSearchIndex 按编号封口。仅在内存索引不可用或超阈值时改走整表重建。
var (
	sealSearchIndex           = filmsnapshot.ResealSearchIndex
	sealOwnedSearchIndex      = filmsnapshot.ResealOwnedSearchIndex
	defaultSealSearchIndexPtr = reflect.ValueOf(filmsnapshot.ResealSearchIndex).Pointer()
	defaultSealOwnedIndexPtr  = reflect.ValueOf(filmsnapshot.ResealOwnedSearchIndex).Pointer()
)

func applySearchReseal(verifiedPrimary string, mids []int64) bool {
	if verifiedPrimary != "" &&
		(reflect.ValueOf(sealOwnedSearchIndex).Pointer() != defaultSealOwnedIndexPtr ||
			reflect.ValueOf(sealSearchIndex).Pointer() == defaultSealSearchIndexPtr) {
		return sealOwnedSearchIndex(verifiedPrimary, mids)
	}
	return sealSearchIndex(mids)
}

// sealRetryWait 所属站查询失败时，编号放回队列后隔一会儿再封。
var sealRetryWait = 2 * time.Second

const maxSealRetryWait = time.Minute

// collectQueueCount 正在跑的采集队列。封口要等每一条队列结束，不能只看当前没有站在抓页。
// frozen 只在本轮已经没有队列、准备落地封口时置位。还有队列时新队列可以加入这一轮。
func (q *searchResealQueue) addSealing(sourceID string) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	q.mu.Lock()
	q.sealing[sourceID] = struct{}{}
	q.stopRetryTimerLocked()
	q.awaitDone = false
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

// schedulePrimarySearchReseal 只把当前主站的编号放进封口队列。
// 所属站查询失败时编号先入队，封口时再查。查失败会把编号放回队列重试。
func schedulePrimarySearchReseal(mids []int64) {
	primary, owned, err := filmsnapshot.PrimarySearchMIDsWithSource(mids)
	if err != nil {
		log.Printf("[Spider] 所属站查询失败，编号留在封口队列稍后重试 mids=%d err=%v", len(mids), err)
		scheduleSearchReseal(mids)
		return
	}
	scheduleSearchResealWithPrimary(primary, owned)
}

func scheduleSearchReseal(mids []int64) {
	scheduleSearchResealWithPrimary("", mids)
}

func scheduleSearchResealWithPrimary(verifiedPrimary string, mids []int64) {
	q := searchReseal
	q.mu.Lock()
	added := q.addPendingLocked(mids, verifiedPrimary)
	if added == 0 {
		q.mu.Unlock()
		return
	}
	q.stopRetryTimerLocked()
	q.awaitDone = false
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
		if len(q.pending) == 0 && len(q.sealing) == 0 {
			q.running = false
			q.retryWait = 0
			q.mu.Unlock()
			return
		}
		q.mu.Unlock()

		if !waitSealSourcesIdle() {
			continue
		}
		if !freezeCollectQueuesIfIdle() {
			waitSealQueuesIdle()
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
	if len(q.pending) == 0 && len(q.sealing) == 0 {
		q.running = false
		q.retryWait = 0
		q.mu.Unlock()
		log.Printf("[Spider] 封口轮次为空，不重建检索索引")
		return true
	}
	pendingN, sealingN := len(q.pending), len(q.sealing)
	mids, verifiedPrimary := q.takePendingLocked()
	q.mu.Unlock()
	log.Printf("[Spider] 开始封口检索索引 pending=%d sealing=%d", pendingN, sealingN)

	sealed := true
	rebuildGenBefore := filmsnapshot.SearchRebuildGen()
	progressBefore := filmsnapshot.SearchIndexProgressToken()
	if len(mids) > 0 {
		searchSealRunning.Store(true)
		sealed = applySearchReseal(verifiedPrimary, mids)
	}
	// 封口函数决定整表加载时会排队重建。已交出的站点等重建结束再收口。
	// 重建期间新进来的编号还在 pending，继续封。
	if filmsnapshot.IsSearchIndexBuilding() || filmsnapshot.SearchRebuildScheduled() {
		searchSealRunning.Store(false)
		parked := false
		// 先拿重建锁再拿 q.mu，和重建收尾通知监听的顺序一致。
		filmsnapshot.WithSearchRebuildScheduled(func(scheduled bool) {
			q.mu.Lock()
			defer q.mu.Unlock()
			if len(q.pending) > 0 || !(scheduled || filmsnapshot.IsSearchIndexBuilding()) {
				return
			}
			q.running = false
			if q.rebuildRetries == 0 {
				q.retryWait = 0
			}
			q.awaitDone = true
			parked = true
		})
		return parked
	}
	if !sealed {
		tok := filmsnapshot.SearchIndexProgressToken()
		// 仅当真正发起了全量重建，或外部进度已进入实质构建/终态（排除 waiting 占位）时，才认为重建已接管。
		handedOff := filmsnapshot.SearchRebuildGen() != rebuildGenBefore ||
			(tok != progressBefore && tok != nil && tok.Status != "waiting")
		if handedOff {
			searchSealRunning.Store(false)
			if tok != nil && tok.Status == "done" {
				q.resetSealRetryWait()
				log.Printf("[Spider] 封口未写入检索索引，重建已完成，交由下一轮收口 mids=%d", len(mids))
				return false
			}
			// 全量重建已快速失败并回退至 idle，禁止直接标完成，转入带上限的重试收敛。
			q.mu.Lock()
			q.running = false
			q.sealRetries = 0
			q.scheduleIdleRebuildRetryLocked()
			q.mu.Unlock()
			log.Printf("[Spider] 封口触发的重建已失败回退，调度退避重试 mids=%d", len(mids))
			return true
		}
		searchSealRunning.Store(false)
		q.mu.Lock()
		q.sealRetries++
		exhausted := q.sealRetries >= maxRebuildSealingRetries
		if exhausted {
			q.running = false
			q.sealRetries = 0
			q.retryWait = 0
		}
		q.mu.Unlock()
		if exhausted {
			log.Printf("[Spider] 封口重试达到上限(%d)，标记检索失败并释放准入 mids=%d", maxRebuildSealingRetries, len(mids))
			q.finishSealingCycle(true)
			return true
		}
		q.restorePending(mids, verifiedPrimary)
		retryWait := q.nextSealRetryWait()
		log.Printf("[Spider] 封口未写入检索索引，编号留在队列稍后重试 mids=%d retry_in=%s", len(mids), retryWait)
		unfreezeCollectQueues()
		if retryWait > 0 {
			time.Sleep(retryWait)
		}
		return false
	}
	searchSealRunning.Store(false)
	q.resetSealRetryWait()
	// 按编号封口成功时 ResealSearchIndex 已发过 done；这里只收口 sealing，不再依赖全局监听。
	q.complete()
	if len(mids) == 0 {
		// 没有编号也要发一次终态，让前端把待检索收成完成。
		filmsnapshot.NotifySearchIndexProgress(0, 0, "done")
	}
	return false
}

func (q *searchResealQueue) advanceRetryWaitLocked(fallback time.Duration) time.Duration {
	wait := q.retryWait
	if wait <= 0 {
		wait = sealRetryWait
	}
	if wait <= 0 {
		wait = fallback
	}
	if wait <= 0 {
		return 0
	}
	if wait > maxSealRetryWait {
		wait = maxSealRetryWait
	}
	q.retryWait = min(wait*2, maxSealRetryWait)
	return wait
}

func (q *searchResealQueue) nextSealRetryWait() time.Duration {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.advanceRetryWaitLocked(0)
}

func (q *searchResealQueue) resetSealRetryWait() {
	q.mu.Lock()
	q.retryWait = 0
	q.sealRetries = 0
	q.mu.Unlock()
}

func (q *searchResealQueue) addPendingLocked(mids []int64, verifiedPrimary string) int {
	verifiedPrimary = strings.TrimSpace(verifiedPrimary)
	wasEmpty := len(q.pending) == 0
	added := 0
	for _, mid := range mids {
		if mid > 0 {
			q.pending[mid] = struct{}{}
			added++
		}
	}
	if added == 0 {
		return 0
	}
	if verifiedPrimary == "" {
		q.allPendingVerified = false
		q.verifiedPrimary = ""
	} else if wasEmpty {
		q.allPendingVerified = true
		q.verifiedPrimary = verifiedPrimary
	} else if !q.allPendingVerified || q.verifiedPrimary != verifiedPrimary {
		q.allPendingVerified = false
		q.verifiedPrimary = ""
	}
	return added
}

func (q *searchResealQueue) restorePending(mids []int64, verifiedPrimary string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.addPendingLocked(mids, verifiedPrimary)
}

func (q *searchResealQueue) takePendingLocked() ([]int64, string) {
	mids := make([]int64, 0, len(q.pending))
	for mid := range q.pending {
		mids = append(mids, mid)
	}
	verifiedPrimary := ""
	if q.allPendingVerified {
		verifiedPrimary = q.verifiedPrimary
	}
	q.pending = map[int64]struct{}{}
	q.verifiedPrimary = ""
	q.allPendingVerified = false
	return mids, verifiedPrimary
}

// complete 把当前等检索的站点收成完成。已经重开的站点保持原状。
func (q *searchResealQueue) complete() {
	q.finishSealingCycle(false)
}

func (q *searchResealQueue) finishSealingCycle(failed bool) {
	q.mu.Lock()
	ids := make([]string, 0, len(q.sealing))
	for id := range q.sealing {
		ids = append(ids, id)
	}
	q.sealing = map[string]struct{}{}
	q.stopRetryTimerLocked()
	q.rebuildRetries = 0
	q.mu.Unlock()
	if failed {
		progress.MarkSealFailed(ids)
	} else {
		progress.MarkSealed(ids)
	}
	if len(ids) > 0 && !IsCollecting() {
		triggerCollectDoneHooks()
	}
}

// onSearchIndexTerminal 仅在停等本轮重建时处理终态：done 收口，idle 调度重试避免永久卡在待检索。
func (q *searchResealQueue) onSearchIndexTerminal(status string) {
	q.mu.Lock()
	if !q.awaitDone {
		q.mu.Unlock()
		return
	}
	q.awaitDone = false
	if q.running || len(q.pending) > 0 {
		q.mu.Unlock()
		return
	}
	if status == "done" {
		q.stopRetryTimerLocked()
		q.rebuildRetries = 0
		q.mu.Unlock()
		q.complete()
		return
	}
	q.scheduleIdleRebuildRetryLocked()
	q.mu.Unlock()
}

const maxRebuildSealingRetries = 5

func (q *searchResealQueue) stopRetryTimerLocked() {
	if q.retryTimer != nil {
		q.retryTimer.Stop()
		q.retryTimer = nil
	}
}

// scheduleIdleRebuildRetryLocked 索引全量重建失败回退 idle 时调度带退避的重试，
// 连续失败达到上限后释放待检索站点，避免站点永久卡死在 StatusSealing。
func (q *searchResealQueue) scheduleIdleRebuildRetryLocked() {
	if len(q.sealing) == 0 {
		q.rebuildRetries = 0
		return
	}
	if q.rebuildRetries >= maxRebuildSealingRetries {
		q.rebuildRetries = 0
		q.stopRetryTimerLocked()
		syslog.Errorf("[Spider] 检索索引重建连续 %d 次失败，释放待检索站点状态，请手动触发索引重建", maxRebuildSealingRetries)
		go q.finishSealingCycle(true)
		return
	}
	wait := q.advanceRetryWaitLocked(2 * time.Second)
	q.rebuildRetries++
	q.stopRetryTimerLocked()
	q.retryTimer = time.AfterFunc(wait, q.retryAfterIdle)
	log.Printf("[Spider] 检索索引重建失败，%s 后重试封口收尾 (%d/%d)", wait, q.rebuildRetries, maxRebuildSealingRetries)
}

func (q *searchResealQueue) retryAfterIdle() {
	q.mu.Lock()
	if len(q.sealing) == 0 || q.running || q.awaitDone {
		q.mu.Unlock()
		return
	}
	q.retryTimer = nil
	if db.Mdb == nil {
		// 无库时无法重建，仍需走退避重试并在达上限后释放待检索站点，避免永久卡在 StatusSealing。
		q.scheduleIdleRebuildRetryLocked()
		q.mu.Unlock()
		return
	}
	// 请求重建前先置 awaitDone，避免极端情况下终态事件在回调前到达而被忽略。
	q.awaitDone = true
	q.mu.Unlock()

	filmsnapshot.RequestActiveFilmSearchIndexRebuild()
	filmsnapshot.WithSearchRebuildScheduled(q.reconcileRetrySchedule)
}

func (q *searchResealQueue) reconcileRetrySchedule(scheduled bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.awaitDone {
		return
	}
	if len(q.sealing) == 0 || q.running {
		q.awaitDone = false
		return
	}
	if !scheduled && !filmsnapshot.IsSearchIndexBuilding() {
		q.awaitDone = false
		q.scheduleIdleRebuildRetryLocked()
	}
}
