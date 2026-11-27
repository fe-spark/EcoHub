package engine

import (
	"context"
	"sync"
)

// fetchStreak 连续抓取失败计数器：满 20 页先停领并暂停一次，恢复后再连续失败 20 页则彻底中断。
type fetchStreak struct {
	mu         sync.Mutex
	cond       *sync.Cond
	ctx        context.Context
	ok         int
	streak     int
	inFlight   int
	holdIntake bool
	pausing    bool
	paused     bool
	stopping   bool
}

func newFetchStreak(ctx context.Context) (*fetchStreak, func() bool) {
	s := &fetchStreak{ctx: ctx}
	s.cond = sync.NewCond(&s.mu)
	stop := context.AfterFunc(ctx, func() {
		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
	})
	return s, stop
}

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

func (s *fetchStreak) finish(ok bool) (stop bool, pause bool) {
	s.mu.Lock()
	if s.inFlight > 0 {
		s.inFlight--
	}
	if s.stopping {
		s.mu.Unlock()
		return false, false
	}
	if ok {
		s.ok++
		s.streak = 0
		if s.holdIntake && !s.pausing {
			s.holdIntake = false
			s.cond.Broadcast()
		}
		s.mu.Unlock()
		return false, false
	}
	s.streak++
	if s.streak < consecutiveFetchFailLimit {
		s.mu.Unlock()
		return false, false
	}
	s.holdIntake = true
	if !s.paused {
		s.paused = true
		s.pausing = true
		s.streak = 0
		s.cond.Broadcast()
		s.mu.Unlock()
		return false, true
	}
	if s.inFlight == 0 {
		s.stopping = true
		stop = true
		s.cond.Broadcast()
	}
	s.mu.Unlock()
	return stop, false
}

func (s *fetchStreak) resumeIntake() {
	s.mu.Lock()
	s.streak = 0
	s.pausing = false
	s.holdIntake = false
	s.cond.Broadcast()
	s.mu.Unlock()
}

func (s *fetchStreak) abandon() bool {
	s.mu.Lock()
	if s.inFlight > 0 {
		s.inFlight--
	}
	shouldStop := false
	if s.holdIntake && s.paused && !s.pausing && s.inFlight == 0 && !s.stopping {
		s.stopping = true
		shouldStop = true
	}
	s.cond.Broadcast()
	s.mu.Unlock()
	return shouldStop
}

// collectFetch 本轮抓取可取消上下文，暂停时取消在途请求，恢复后重建。
type collectFetch struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

func newCollectFetch(parent context.Context) *collectFetch {
	ctx, cancel := context.WithCancel(parent)
	return &collectFetch{ctx: ctx, cancel: cancel}
}

func (f *collectFetch) current() context.Context {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ctx
}

func (f *collectFetch) stopRequests() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancel()
}

func (f *collectFetch) resume(parent context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancel()
	f.ctx, f.cancel = context.WithCancel(parent)
}

// emptyPageStreak 按页码有序水位线统计连续空页，避免慢页在途时被后续空页提前截断。
type emptyPageStreak struct {
	mu       sync.Mutex
	limit    int
	frontier int
	streak   int
	hadData  bool
	done     map[int]bool
}

func newEmptyPageStreak(enabled bool, limit int) *emptyPageStreak {
	if !enabled || limit <= 0 {
		return nil
	}
	return &emptyPageStreak{
		limit: limit,
		done:  make(map[int]bool),
	}
}

func (e *emptyPageStreak) record(page int, empty, hasData bool) (reached bool, hadData bool) {
	if e == nil || page <= 0 {
		return false, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if hasData {
		e.hadData = true
	}
	if page > e.frontier {
		e.done[page] = empty
		for {
			isEmpty, ok := e.done[e.frontier+1]
			if !ok {
				break
			}
			delete(e.done, e.frontier+1)
			e.frontier++
			if isEmpty {
				e.streak++
			} else {
				e.streak = 0
			}
		}
	}
	return e.streak >= e.limit, e.hadData
}
