package scheduler

import (
	"log"
	"sort"
	"strings"
	"time"

	"server/internal/config"
)

const writeAdaptWindow = 4

type writePace struct {
	samples  []time.Duration
	prev     time.Duration
	lockSeen bool
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
	next, reason, changed := l.pace.observe(prev, cost, isWriteLockErr(err))
	if changed {
		l.writeLimit = next
	}
	follow := changed && l.followConfig
	l.mu.Unlock()
	if !changed {
		return
	}
	if follow {
		config.ApplyWriteSlots(next)
		l.mu.Lock()
		l.maxPerSource = config.CollectWriteMaxPendingPagesPerSource
		l.maxGlobal = config.CollectWriteMaxPendingPagesGlobal
		l.cond.Broadcast()
		l.mu.Unlock()
	} else {
		l.mu.Lock()
		l.cond.Broadcast()
		l.mu.Unlock()
	}
	log.Printf("[Spider][WriteScheduler] %s 写并发 %d→%d 最近耗时=%s 原因=%s",
		l.name, prev, next, cost, reason)
}

func (p *writePace) observe(limit int, cost time.Duration, lockErr bool) (int, string, bool) {
	if limit < config.CollectWriteSlotMin {
		limit = config.CollectWriteSlotMin
	}
	if lockErr {
		p.lockSeen = true
		p.samples = nil
		if limit > config.CollectWriteSlotMin {
			return limit - 1, "锁等待", true
		}
		return limit, "", false
	}
	if cost <= 0 {
		return limit, "", false
	}
	p.samples = append(p.samples, cost)
	if len(p.samples) < writeAdaptWindow {
		return limit, "", false
	}
	med := medianDuration(p.samples)
	prev := p.prev
	locked := p.lockSeen
	p.prev = med
	p.lockSeen = false
	p.samples = nil
	if locked {
		return limit, "", false
	}
	if prev > 0 && med > prev*2 && limit > config.CollectWriteSlotMin {
		return limit - 1, "变慢", true
	}
	if limit < config.CollectWriteSlotMax {
		return limit + 1, "上探", true
	}
	return limit, "", false
}

func medianDuration(samples []time.Duration) time.Duration {
	cp := append([]time.Duration(nil), samples...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	return cp[len(cp)/2]
}

func isWriteLockErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "deadlock found") || strings.Contains(msg, "lock wait timeout") ||
		strings.Contains(msg, "error 1213") || strings.Contains(msg, "error 1205")
}
