package fetcher

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"server/internal/config"
	"server/internal/model"
	"server/internal/spider/progress"
	"server/internal/spider/scheduler"
	"server/internal/utils"
)

// pauseTestEnv 单 worker、可调暂停参数，按页号决定成功或失败，并记下每页的请求时间。
type pauseTestEnv struct {
	mu        sync.Mutex
	requested []int
	at        map[int]time.Time
}

func (e *pauseTestEnv) pages() []int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]int(nil), e.requested...)
}

func setupPauseTest(t *testing.T, streak int, pause time.Duration, maxTimes int, fail func(pg int) bool) *pauseTestEnv {
	t.Helper()
	oldDeps := deps
	oldWorkers := config.CollectPageWorkers
	oldStreak, oldPause, oldMax := sourcePauseFailStreak, sourcePauseDuration, sourcePauseMaxTimes
	t.Cleanup(func() {
		deps = oldDeps
		config.CollectPageWorkers = oldWorkers
		sourcePauseFailStreak, sourcePauseDuration, sourcePauseMaxTimes = oldStreak, oldPause, oldMax
	})
	config.CollectPageWorkers = 1
	sourcePauseFailStreak, sourcePauseDuration, sourcePauseMaxTimes = streak, pause, maxTimes

	env := &pauseTestEnv{at: make(map[int]time.Time)}
	deps.WaitTurn = func(context.Context, *model.FilmSource, string) (func(error), error) {
		return func(error) {}, nil
	}
	deps.GetFilmDetail = func(_ *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error) {
		pg, _ := strconv.Atoi(r.Params.Get("pg"))
		env.mu.Lock()
		env.requested = append(env.requested, pg)
		env.at[pg] = time.Now()
		env.mu.Unlock()
		if fail(pg) {
			// 不可重试的错误，避免逐页重试拖慢测试。
			return nil, errors.New("invalid character '<' looking for beginning of value")
		}
		return []model.MovieDetail{{Id: int64(pg), Name: "片"}}, nil
	}
	deps.SavePage = func(context.Context, *model.FilmSource, int, []model.MovieDetail) (scheduler.Mids, error) {
		return scheduler.Mids{}, nil
	}
	return env
}

// 有成功页的站连续失败到阈值：整站暂停，到点从断点继续，失败页照常记为失败。
func TestCollectPages_FailStreakPausesThenResumes(t *testing.T) {
	env := setupPauseTest(t, 3, 150*time.Millisecond, 3, func(pg int) bool { return pg >= 2 && pg <= 4 })
	source := &model.FilmSource{Id: "pause_resume", Name: "限流站"}

	_, succ, fail, failedPages, err := CollectPages(context.Background(), 10, nil, source, 24, nil)
	if succ != 7 || fail != 3 {
		t.Fatalf("succ=%d fail=%d, want 7/3", succ, fail)
	}
	if len(failedPages) != 3 || failedPages[0] != 2 || failedPages[2] != 4 {
		t.Fatalf("failedPages=%v want [2 3 4]", failedPages)
	}
	if err == nil {
		t.Fatal("failed pages should surface as error")
	}
	if got := env.pages(); len(got) != 10 {
		t.Fatalf("all pages should be requested after resume, got %v", got)
	}
	env.mu.Lock()
	gap := env.at[5].Sub(env.at[4])
	env.mu.Unlock()
	if gap < 150*time.Millisecond {
		t.Fatalf("page 5 requested %v after page 4, want a pause >= 150ms", gap)
	}
}

// 连续失败中间夹一页成功就清零，不会暂停（暂停时长设成 1 小时，一旦暂停测试就会超时）。
func TestCollectPages_SuccessResetsFailStreak(t *testing.T) {
	env := setupPauseTest(t, 3, time.Hour, 3, func(pg int) bool { return pg != 1 && pg%3 != 1 })
	source := &model.FilmSource{Id: "pause_reset", Name: "抖动站"}

	done := make(chan struct{})
	var succ, fail int
	go func() {
		defer close(done)
		_, succ, fail, _, _ = CollectPages(context.Background(), 30, nil, source, 24, nil)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("source paused although every streak was broken by a success")
	}
	if succ != 10 || fail != 20 || len(env.pages()) != 30 {
		t.Fatalf("succ=%d fail=%d requested=%d, want 10/20/30", succ, fail, len(env.pages()))
	}
}

// 连续暂停用完仍连续失败：结束本站这一轮，后面的页不再请求。
func TestCollectPages_GiveUpAfterMaxPauses(t *testing.T) {
	env := setupPauseTest(t, 3, 20*time.Millisecond, 3, func(pg int) bool { return pg != 1 })
	source := &model.FilmSource{Id: "pause_give_up", Name: "封 IP 站"}

	_, succ, fail, failedPages, err := CollectPages(context.Background(), 100, nil, source, 24, nil)
	// 1 页成功 + 3 次暂停前各 3 页 + 第 4 轮 3 页后放弃。
	if got := env.pages(); len(got) != 13 {
		t.Fatalf("requested %d pages (%v), want 13 and none after giving up", len(got), got)
	}
	if succ != 1 || fail != 12 || len(failedPages) != 12 {
		t.Fatalf("succ=%d fail=%d failedPages=%d, want 1/12/12", succ, fail, len(failedPages))
	}
	if err == nil || !strings.Contains(err.Error(), "连续暂停 3 次") {
		t.Fatalf("expected give-up error, got %v", err)
	}
}

// 暂停中停止采集：立刻返回，不等暂停结束。
func TestCollectPages_StopDuringPauseReturnsPromptly(t *testing.T) {
	env := setupPauseTest(t, 3, time.Hour, 3, func(pg int) bool { return pg != 1 })
	source := &model.FilmSource{Id: "pause_stop", Name: "暂停中停止"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, _, _, err := CollectPages(ctx, 100, nil, source, 24, nil)
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for len(env.pages()) < 4 {
		if time.Now().After(deadline) {
			t.Fatal("pause never reached")
		}
		time.Sleep(5 * time.Millisecond)
	}
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stop during pause did not return promptly")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("stop took %v", elapsed)
	}
	if got := env.pages(); len(got) != 4 {
		t.Fatalf("no page should be requested during the pause, got %v", got)
	}
}

// 失败页不再截断：超过 500 页的失败全部记进结果和实时进度，重试失败页能拿到全部页码。
func TestCollectPages_RecordsAllFailedPages(t *testing.T) {
	setupPauseTest(t, 1<<30, time.Hour, 3, func(pg int) bool { return pg != 1 })
	source := &model.FilmSource{Id: "pause_many_failed", Name: "大量失败"}
	progress.MarkSourcesCollectStarting([]model.FilmSource{*source}, "q-many-failed")

	_, succ, fail, failedPages, _ := CollectPages(context.Background(), 1201, nil, source, 24, nil)
	if succ != 1 || fail != 1200 || len(failedPages) != 1200 {
		t.Fatalf("succ=%d fail=%d failedPages=%d, want 1/1200/1200", succ, fail, len(failedPages))
	}
	snap, _ := progress.Snapshot(source.Id)
	if len(snap.FailedPages) != 1200 {
		t.Fatalf("progress failedPages=%d want 1200", len(snap.FailedPages))
	}

	// 重试失败页：把这 1200 页当目标页重采，每一页都会被请求。
	retryEnv := setupPauseTest(t, 1<<30, time.Hour, 3, func(int) bool { return false })
	_, succ, fail, _, err := CollectPages(context.Background(), 1201, failedPages, source, 24, nil)
	if err != nil || succ != 1200 || fail != 0 || len(retryEnv.pages()) != 1200 {
		t.Fatalf("retry succ=%d fail=%d requested=%d err=%v, want all 1200 pages", succ, fail, len(retryEnv.pages()), err)
	}
}
