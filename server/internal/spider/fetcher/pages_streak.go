package fetcher

import (
	"context"
	"log"
	"sync"
)

// fetchStreak 连续抓取失败。满 20 页后先停领新页，等已经发出的请求结束。
// 其中有成功就清零并继续。全部失败才通知调用方中断。
type fetchStreak struct {
	mu         sync.Mutex
	cond       *sync.Cond
	ctx        context.Context
	name       string
	ok         int
	streak     int
	inFlight   int
	holdIntake bool
	// paused 已经用过一次限流暂停。恢复后再连续失败就终止，不再暂停。
	paused   bool
	stopping bool
}

func newFetchStreak(ctx context.Context, name string) (*fetchStreak, func() bool) {
	s := &fetchStreak{ctx: ctx, name: name}
	s.cond = sync.NewCond(&s.mu)
	stop := context.AfterFunc(ctx, func() {
		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
	})
	return s, stop
}

func (s *fetchStreak) succeeded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ok > 0
}

// tryBegin 可以领新页时占一个在途名额。正在等在途结果，或已经决定中断时返回 false。
func (s *fetchStreak) tryBegin() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.holdIntake && !s.stopping && s.ctx.Err() == nil {
		s.cond.Wait()
	}
	if s.stopping || s.ctx.Err() != nil {
		return false
	}
	s.inFlight++
	return true
}

// finish 结束一次抓取。
// 第一个返回值表示在途请求都失败了，应该中断。
// 第二个返回值表示该停下来暂停一次，暂停结束后继续采集。
func (s *fetchStreak) finish(ok bool) (stop bool, pause bool) {
	s.mu.Lock()
	if s.inFlight > 0 {
		s.inFlight--
	}
	if s.stopping {
		s.mu.Unlock()
		return false, false
	}
	cleared := false
	waiting := 0
	if ok {
		s.ok++
		s.streak = 0
		if s.holdIntake {
			s.holdIntake = false
			cleared = true
			s.cond.Broadcast()
		}
		s.mu.Unlock()
		if cleared {
			log.Printf("[Spider] 站点 %s 在途抓取成功，连续失败已清零，继续采集", s.name)
		}
		return false, false
	}
	s.streak++
	startedHold := false
	if s.streak >= consecutiveFetchFailLimit {
		if !s.holdIntake && s.inFlight > 0 {
			startedHold = true
			waiting = s.inFlight
		}
		s.holdIntake = true
	}
	if s.holdIntake && s.inFlight == 0 {
		if s.paused {
			s.stopping = true
			stop = true
		} else {
			// 只暂停这一次。连续计数留给恢复后重新算。
			s.paused = true
			s.streak = 0
			s.holdIntake = false
			pause = true
		}
		s.cond.Broadcast()
	}
	s.mu.Unlock()
	if startedHold {
		log.Printf("[Spider] 站点 %s 连续 %d 页抓取失败，停止领新页，等待 %d 个在途请求", s.name, consecutiveFetchFailLimit, waiting)
	}
	return stop, pause
}

// abandon 这次请求被取消，不计入连续失败。返回 true 表示其余在途也已失败，应该中断。
func (s *fetchStreak) abandon() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inFlight > 0 {
		s.inFlight--
	}
	s.cond.Broadcast()
	if s.stopping || s.ctx.Err() != nil {
		return false
	}
	if s.holdIntake && s.inFlight == 0 && s.streak >= consecutiveFetchFailLimit && s.paused {
		s.stopping = true
		return true
	}
	return false
}
