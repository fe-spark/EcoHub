package fetcher

import (
	"context"
	"sync"
)

// fetchStreak 连续抓取失败。满 20 页后立刻停领新页并暂停一次。
// 暂停期间在途请求会被取消。恢复后再连续失败就终止，不再暂停。
type fetchStreak struct {
	mu         sync.Mutex
	cond       *sync.Cond
	ctx        context.Context
	name       string
	ok         int
	streak     int
	inFlight   int
	holdIntake bool
	// pausing 正在停这 2 分钟。这段时间不能重新领页。
	pausing bool
	// paused 已经用过一次限流暂停。
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
// 第一个返回值表示暂停后再次连续失败，应该中断。
// 第二个返回值表示现在就暂停一次，调用方要取消在途请求。
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

// resumeIntake 限流暂停已经结束，允许继续领页。
// 暂停前已经返回的失败留在失败列表里，但不占恢复后的 20 次额度。
func (s *fetchStreak) resumeIntake() {
	s.mu.Lock()
	s.streak = 0
	s.pausing = false
	s.holdIntake = false
	s.cond.Broadcast()
	s.mu.Unlock()
}

// abandon 这次请求被取消，不计入连续失败。
// 若处于暂停恢复后的中止态且在途已归零，置 stopping 并通知调用方取消上下文唤醒 tryBegin。
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

// collectFetch 本站这一轮的请求上下文。限流时取消它，恢复后再换一个。
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
	f.ctx, f.cancel = context.WithCancel(parent)
}

// emptyPageStreak 按页码顺序推进连续空页水位线，避免慢页在途时被后续空页误截断。
type emptyPageStreak struct {
	mu       sync.Mutex
	limit    int
	frontier int
	streak   int
	hadData  bool
	done     map[int]bool // true=空页，false=非空或失败
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

// record 上报单页抓取结果，返回是否达到连续空页阈值以及本轮是否已有非空数据。
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
