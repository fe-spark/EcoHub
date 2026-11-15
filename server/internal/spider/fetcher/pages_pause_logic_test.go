package fetcher

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"server/internal/config"
	"server/internal/model"
	"server/internal/spider/progress"
)

func failOne(t *testing.T, streak *fetchStreak) (stop bool, pause bool) {
	t.Helper()
	if !streak.tryBegin() {
		t.Fatal("tryBegin returned false")
	}
	return streak.finish(false)
}

func drivePause(t *testing.T, streak *fetchStreak) {
	t.Helper()
	for i := 0; i < consecutiveFetchFailLimit-1; i++ {
		stop, pause := failOne(t, streak)
		if stop || pause {
			t.Fatalf("failure %d: stop=%v pause=%v", i+1, stop, pause)
		}
	}
	stop, pause := failOne(t, streak)
	if stop || !pause {
		t.Fatalf("failure %d: stop=%v pause=%v, want pause", consecutiveFetchFailLimit, stop, pause)
	}
}

// 第 20 页失败后先暂停。暂停期间其他工人拿不到新页，暂停结束后才能拿。
// 再连续失败则中断，不会再暂停一次。
func TestFetchStreak_PauseThenSecondRoundStops(t *testing.T) {
	streak, stopWait := newFetchStreak(context.Background(), "暂停站")
	defer stopWait()
	drivePause(t, streak)

	got := make(chan bool, 1)
	go func() {
		got <- streak.tryBegin()
	}()
	select {
	case <-got:
		t.Fatal("paused streak still handed out a page")
	case <-time.After(40 * time.Millisecond):
	}
	streak.resumeIntake()
	var resumed bool
	select {
	case resumed = <-got:
	case <-time.After(time.Second):
		t.Fatal("resume did not unblock the waiter")
	}
	if !resumed {
		t.Fatal("resume returned false")
	}
	stop, pause := streak.finish(false)
	if stop || pause {
		t.Fatalf("first failure after resume: stop=%v pause=%v", stop, pause)
	}
	var stopped bool
	for i := 0; i < consecutiveFetchFailLimit-1; i++ {
		stop, pause = failOne(t, streak)
		if pause {
			t.Fatal("second round paused again")
		}
		if stop {
			stopped = true
		}
	}
	if !stopped {
		t.Fatal("second round did not stop")
	}
}

// 第 20 页失败时，另一页还在途也立刻暂停。这一页随后成功，也不能在暂停期间重新领页。
func TestFetchStreak_InFlightSuccessDoesNotReopenPause(t *testing.T) {
	streak, stopWait := newFetchStreak(context.Background(), "限流站")
	defer stopWait()
	for i := 0; i < consecutiveFetchFailLimit-1; i++ {
		stop, pause := failOne(t, streak)
		if stop || pause {
			t.Fatalf("failure %d: stop=%v pause=%v", i+1, stop, pause)
		}
	}
	if !streak.tryBegin() || !streak.tryBegin() {
		t.Fatal("could not start the last two requests")
	}
	stop, pause := streak.finish(false)
	if stop || !pause {
		t.Fatalf("in-flight failure: stop=%v pause=%v, want pause", stop, pause)
	}
	stop, pause = streak.finish(true)
	if stop || pause {
		t.Fatalf("late success: stop=%v pause=%v", stop, pause)
	}
	got := make(chan bool, 1)
	go func() {
		got <- streak.tryBegin()
	}()
	select {
	case <-got:
		t.Fatal("late success reopened intake during the pause")
	case <-time.After(40 * time.Millisecond):
	}
}

// 采集取消时，正在等待的工人要马上退出，不能记成连续失败中断。
func TestFetchStreak_CancelUnblocksAndDoesNotStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	streak, stopWait := newFetchStreak(ctx, "取消站")
	defer stopWait()
	drivePause(t, streak)

	cancel()
	start := time.Now()
	if streak.tryBegin() {
		t.Fatal("tryBegin succeeded after cancel")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("cancel took %s to unblock", time.Since(start))
	}
	streak.abandon()
}

func watchPauses(sourceID string, stop <-chan struct{}) *atomic.Int32 {
	var pauses atomic.Int32
	go func() {
		prev := ""
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				snap, ok := progress.Snapshot(sourceID)
				if !ok {
					continue
				}
				if snap.Status == progress.StatusPaused && prev != progress.StatusPaused {
					pauses.Add(1)
				}
				prev = snap.Status
			}
		}
	}()
	return &pauses
}

// 失败页不到 20，或多页刚好在第 20 页取完，都不应干等整段暂停。
func TestCollectPages_NoPauseWhenWorkIsDone(t *testing.T) {
	prev := fetchFailPause
	fetchFailPause = 3 * time.Second
	t.Cleanup(func() { fetchFailPause = prev })

	cases := []struct {
		name  string
		pages int
		works int
	}{
		{name: "19页1个工人", pages: 19, works: 1},
		{name: "20页1个工人", pages: 20, works: 1},
		{name: "指定20页", pages: 20, works: 1},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := setupFetchTest(t, func(int) bool { return true })
			config.CollectPageWorkers = tc.works
			source := &model.FilmSource{Id: "no_pause_" + string(rune('a'+i)), Name: tc.name}
			var targets []int
			if strings.Contains(tc.name, "指定") {
				targets = make([]int, tc.pages)
				for n := range targets {
					targets[n] = n + 1
				}
			}
			start := time.Now()
			_, _, fail, failedPages, err := CollectPages(context.Background(), tc.pages, targets, source, 24, nil)
			if time.Since(start) > time.Second {
				t.Fatalf("waited %s, pause ran with no pages left", time.Since(start))
			}
			if fail != tc.pages || len(failedPages) != tc.pages {
				t.Fatalf("fail=%d pages=%d, want %d", fail, len(failedPages), tc.pages)
			}
			if err == nil || strings.Contains(err.Error(), "中断") {
				t.Fatalf("err=%v", err)
			}
			_, turns := env.snapshot()
			if turns != tc.pages*3 {
				t.Fatalf("turns=%d, want %d", turns, tc.pages*3)
			}
		})
	}
}

// 后面还有页时只暂停一次。暂停中不再发请求，暂停后再连续失败才中断。
func TestCollectPages_OnePauseThenInterrupt(t *testing.T) {
	prev := fetchFailPause
	fetchFailPause = 200 * time.Millisecond
	t.Cleanup(func() { fetchFailPause = prev })

	env := setupFetchTest(t, func(int) bool { return true })
	config.CollectPageWorkers = 4
	source := &model.FilmSource{Id: "one_pause", Name: "只暂停一次"}
	progress.MarkSourcesCollectStarting([]model.FilmSource{*source}, "q-one-pause")
	stopWatch := make(chan struct{})
	pauses := watchPauses(source.Id, stopWatch)

	start := time.Now()
	_, _, fail, _, err := CollectPages(context.Background(), 80, nil, source, 24, nil)
	close(stopWatch)
	elapsed := time.Since(start)
	if err == nil || err.Error() != interruptReason {
		t.Fatalf("err=%v", err)
	}
	if fail < 40 || fail > 46 {
		t.Fatalf("fail=%d, want 40 to 46", fail)
	}
	if pauses.Load() != 1 {
		t.Fatalf("pauses=%d, want 1", pauses.Load())
	}
	if elapsed < 150*time.Millisecond || elapsed > time.Second {
		t.Fatalf("elapsed=%s, want one pause and no second pause", elapsed)
	}
	_, turns := env.snapshot()
	if turns < fail*3 {
		t.Fatalf("turns=%d fail=%d", turns, fail)
	}
}

// 暂停结束后有一页成功，本轮不再因为连续失败中断。
func TestCollectPages_SuccessAfterPauseDoesNotInterrupt(t *testing.T) {
	prev := fetchFailPause
	fetchFailPause = 80 * time.Millisecond
	t.Cleanup(func() { fetchFailPause = prev })

	env := setupFetchTest(t, func(pg int) bool { return pg <= 20 })
	source := &model.FilmSource{Id: "pause_then_ok", Name: "暂停后成功"}
	progress.MarkSourcesCollectStarting([]model.FilmSource{*source}, "q-pause-ok")

	has, succ, fail, _, err := CollectPages(context.Background(), 40, nil, source, 24, nil)
	if !has || succ != 20 || fail != 20 {
		t.Fatalf("has=%v succ=%d fail=%d", has, succ, fail)
	}
	if err == nil || strings.Contains(err.Error(), "中断") {
		t.Fatalf("err=%v", err)
	}
	pages, _ := env.snapshot()
	if uniquePages(pages) != 40 {
		t.Fatalf("pages=%d, want 40", uniquePages(pages))
	}
}

// 四个工人都在暂停里等待时，点停止要马上返回，并且不是连续失败中断。
func TestCollectPages_CancelDuringPauseWithWorkers(t *testing.T) {
	prev := fetchFailPause
	fetchFailPause = 5 * time.Second
	t.Cleanup(func() { fetchFailPause = prev })

	env := setupFetchTest(t, func(int) bool { return true })
	config.CollectPageWorkers = 4
	source := &model.FilmSource{Id: "cancel_pause_workers", Name: "暂停中停止"}
	progress.MarkSourcesCollectStarting([]model.FilmSource{*source}, "q-cancel-workers")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		fail int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		_, _, fail, _, err := CollectPages(ctx, 100, nil, source, 24, nil)
		done <- result{fail, err}
	}()

	deadline := time.Now().Add(3 * time.Second)
	for {
		snap, ok := progress.Snapshot(source.Id)
		if ok && snap.Status == progress.StatusPaused {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("source did not reach the rate-limit pause")
		}
		time.Sleep(2 * time.Millisecond)
	}
	_, turnsAtPause := env.snapshot()
	cancel()
	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) || strings.Contains(got.err.Error(), "中断") {
			t.Fatalf("err=%v", got.err)
		}
		if got.fail == 0 {
			t.Fatal("failed pages were dropped")
		}
	case <-time.After(time.Second):
		t.Fatal("cancel during pause did not return")
	}
	_, turnsAfter := env.snapshot()
	if turnsAfter != turnsAtPause {
		t.Fatalf("requests continued after cancel: %d -> %d", turnsAtPause, turnsAfter)
	}
}
