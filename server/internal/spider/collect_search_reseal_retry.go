package spider

import (
	"log"
	"time"

	"server/internal/infra/db"
	"server/internal/infra/syslog"
	filmsnapshot "server/internal/repository/film/snapshot"
)

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
	wait := q.retryWait
	if wait <= 0 {
		wait = sealRetryWait
	}
	if wait <= 0 {
		wait = 2 * time.Second
	}
	if wait > maxSealRetryWait {
		wait = maxSealRetryWait
	}
	q.retryWait = min(wait*2, maxSealRetryWait)
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
