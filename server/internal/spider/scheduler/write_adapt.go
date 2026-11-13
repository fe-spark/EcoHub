package scheduler

import (
	"log"
	"strings"
	"time"

	"server/internal/config"
)

const writeAdaptHold = 30 * time.Second

type writePace struct {
	quietUntil time.Time
	now        func() time.Time
}

func (l *writeLane) currentWriteLimitLocked() int {
	if l.writeLimit > 0 {
		return l.writeLimit
	}
	return l.workers
}

func (l *writeLane) noteWrite(cost time.Duration, err error) {
	l.mu.Lock()
	l.lastCost = cost
	if !l.adapt {
		l.mu.Unlock()
		return
	}
	prev := l.writeLimit
	next, reason, changed := l.pace.observe(prev, isWriteLockErr(err))
	if changed {
		l.writeLimit = next
		l.cond.Broadcast()
	}
	l.mu.Unlock()
	if !changed {
		return
	}
	log.Printf("[Spider][WriteScheduler] %s 写并发 %d→%d 原因=%s", l.name, prev, next, reason)
}

// observe 只在锁错误时减 1。连续 30 秒没有锁错误再加 1，直到写槽上限。耗时不参与。
func (p *writePace) observe(limit int, lockErr bool) (int, string, bool) {
	if limit < config.CollectWriteSlotMin {
		limit = config.CollectWriteSlotMin
	}
	if lockErr {
		p.quietUntil = p.at().Add(writeAdaptHold)
		if limit > config.CollectWriteSlotMin {
			return limit - 1, "锁等待", true
		}
		return limit, "", false
	}
	if !p.quietUntil.IsZero() && !p.at().Before(p.quietUntil) && limit < config.WriteSlotMax() {
		p.quietUntil = p.at().Add(writeAdaptHold)
		return limit + 1, "回升", true
	}
	return limit, "", false
}

func (p *writePace) at() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

func isWriteLockErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "deadlock found") || strings.Contains(msg, "lock wait timeout") ||
		strings.Contains(msg, "error 1213") || strings.Contains(msg, "error 1205")
}
