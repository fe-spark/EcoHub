package scheduler

import (
	"log"
	"time"
)

type backpressureReason string

const (
	bpNone       backpressureReason = ""
	bpSourceFull backpressureReason = "source_full"
	bpGlobalFull backpressureReason = "global_full"
)

func (l *writeLane) submitBlockedReason(queue *writeQueue) backpressureReason {
	if len(queue.pending) >= l.maxPendingPerSource() {
		return bpSourceFull
	}
	if l.totalPending >= l.maxPendingGlobal() {
		return bpGlobalFull
	}
	return bpNone
}

func (l *writeLane) shouldLogBackpressureEnter() bool {
	now := time.Now().UnixNano()
	last := l.lastBackpressureLog.Load()
	if last != 0 && now-last < int64(2*time.Second) {
		return false
	}
	return l.lastBackpressureLog.CompareAndSwap(last, now)
}

func (l *writeLane) shouldLogBackpressureLeave() bool {
	now := time.Now().UnixNano()
	last := l.lastBackpressureLeaveLog.Load()
	if last != 0 && now-last < int64(3*time.Second) {
		return false
	}
	return l.lastBackpressureLeaveLog.CompareAndSwap(last, now)
}

func (l *writeLane) logBackpressureLeave(canceled bool, reason backpressureReason, sourceName string, wait time.Duration, sourcePending, globalPending int) {
	status := "resumed"
	if canceled {
		status = "canceled"
	}
	log.Printf("[Spider][WriteScheduler] %s 反压结束 status=%s reason=%s source=%s wait=%s source_pending=%d global_pending=%d/%d",
		l.name, status, reason, sourceName, wait.Round(time.Millisecond), sourcePending, globalPending, l.maxPendingGlobal())
}
