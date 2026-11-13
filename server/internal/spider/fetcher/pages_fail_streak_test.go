package fetcher

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"server/internal/config"
	"server/internal/model"
	"server/internal/spider/progress"
	"server/internal/spider/scheduler"
	"server/internal/utils"
)

const interruptReason = "暂停后仍连续 20 页抓取失败，已中断采集"

func shortenFailPause(t *testing.T) {
	t.Helper()
	prev := fetchFailPause
	fetchFailPause = 20 * time.Millisecond
	t.Cleanup(func() { fetchFailPause = prev })
}

type fetchEnv struct {
	mu    sync.Mutex
	pages []int
	turns int
}

func (e *fetchEnv) snapshot() (pages []int, turns int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]int(nil), e.pages...), e.turns
}

func setupFetchTest(t *testing.T, fail func(pg int) bool) *fetchEnv {
	t.Helper()
	oldDeps := deps
	oldWorkers := config.CollectPageWorkers
	t.Cleanup(func() {
		deps = oldDeps
		config.CollectPageWorkers = oldWorkers
	})
	config.CollectPageWorkers = 1
	env := &fetchEnv{}
	deps = Deps{
		WaitTurn: func(context.Context, *model.FilmSource, string) (func(error), error) {
			env.mu.Lock()
			env.turns++
			env.mu.Unlock()
			return func(error) {}, nil
		},
		GetFilmDetail: func(_ *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error) {
			pg, _ := strconv.Atoi(r.Params.Get("pg"))
			env.mu.Lock()
			env.pages = append(env.pages, pg)
			env.mu.Unlock()
			if fail(pg) {
				return nil, errors.New("invalid character '<' looking for beginning of value")
			}
			return []model.MovieDetail{{Id: int64(pg), Name: "片"}}, nil
		},
		SavePage: func(context.Context, *model.FilmSource, int, []model.MovieDetail) (scheduler.Mids, error) {
			return scheduler.Mids{}, nil
		},
	}
	return env
}

func uniquePages(pages []int) int {
	seen := make(map[int]struct{}, len(pages))
	for _, pg := range pages {
		seen[pg] = struct{}{}
	}
	return len(seen)
}

func countPage(pages []int, pg int) int {
	n := 0
	for _, item := range pages {
		if item == pg {
			n++
		}
	}
	return n
}

type errBatch struct {
	mu     sync.Mutex
	id     string
	reason string
}

func (b *errBatch) IsStandalone() bool                              { return false }
func (b *errBatch) AddAffectedMIDs(*model.FilmSource, int, []int64) {}
func (b *errBatch) AddNotifyMIDs(string, []int64)                   {}
func (b *errBatch) TouchRecord()                                    {}
func (b *errBatch) AddCategorySkip(string, int)                     {}
func (b *errBatch) RecordSourceError(sourceID, reason string) {
	b.mu.Lock()
	b.id = sourceID
	b.reason = reason
	b.mu.Unlock()
}

func (b *errBatch) got() (string, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.id, b.reason
}

// 失败页按采集间隔再请求两次。仍失败就记失败，单页失败不中断。
func TestCollectPages_RetryOnceThenKeepsGoing(t *testing.T) {
	env := setupFetchTest(t, func(pg int) bool { return pg == 2 })
	source := &model.FilmSource{Id: "retry_once", Name: "重试一次"}

	has, succ, fail, failedPages, err := CollectPages(context.Background(), 3, nil, source, 24, nil)
	if !has || succ != 2 || fail != 1 || len(failedPages) != 1 || failedPages[0] != 2 {
		t.Fatalf("has=%v succ=%d fail=%d pages=%v", has, succ, fail, failedPages)
	}
	if err == nil || strings.Contains(err.Error(), "中断") {
		t.Fatalf("single failure should stay a page error, got %v", err)
	}
	pages, turns := env.snapshot()
	if turns != 5 || countPage(pages, 2) != 3 || uniquePages(pages) != 3 {
		t.Fatalf("turns=%d pages=%v, want page 2 fetched 3 times and all 3 pages", turns, pages)
	}
}

// 中间有成功页就清零。连续失败不到 20 页时采完，不中断。
func TestCollectPages_SuccessResetsFailStreak(t *testing.T) {
	env := setupFetchTest(t, func(pg int) bool { return pg%20 != 0 })
	source := &model.FilmSource{Id: "streak_reset", Name: "清零"}
	batch := &errBatch{}

	_, succ, fail, _, err := CollectPages(context.Background(), 40, nil, source, 24, batch)
	if succ != 2 || fail != 38 {
		t.Fatalf("succ=%d fail=%d, want 2/38", succ, fail)
	}
	if err == nil || strings.Contains(err.Error(), "中断") {
		t.Fatalf("err=%v", err)
	}
	if id, reason := batch.got(); id != "" || reason != "" {
		t.Fatalf("record reason=%s %s, want empty", id, reason)
	}
	pages, _ := env.snapshot()
	if uniquePages(pages) != 40 {
		t.Fatalf("requested %d pages, want 40", uniquePages(pages))
	}
}

// 已有成功页后连续 20 页抓取失败：停掉本站，后面的页不再请求，原因写入采集记录。
func TestCollectPages_ConsecutiveFetchFailInterrupts(t *testing.T) {
	shortenFailPause(t)
	env := setupFetchTest(t, func(pg int) bool { return pg != 1 })
	var notified atomic.Int32
	deps.OnSourceFailed = func(string, string, string) { notified.Add(1) }
	source := &model.FilmSource{Id: "streak_partial", Name: "中断站"}
	batch := &errBatch{}
	progress.MarkSourcesCollectStarting([]model.FilmSource{*source}, "q-partial")

	start := time.Now()
	has, succ, fail, failedPages, err := CollectPages(context.Background(), 100, nil, source, 24, batch)
	if time.Since(start) > 2*time.Second {
		t.Fatal("retry still waits beyond the collect interval")
	}
	if !has || succ != 1 || fail != 40 || len(failedPages) != 40 {
		t.Fatalf("has=%v succ=%d fail=%d failedPages=%d", has, succ, fail, len(failedPages))
	}
	if err == nil || err.Error() != interruptReason {
		t.Fatalf("err=%v", err)
	}
	pages, _ := env.snapshot()
	if uniquePages(pages) != 41 {
		t.Fatalf("requested %d pages (%v), want 41", uniquePages(pages), pages)
	}
	if notified.Load() != 0 {
		t.Fatal("a source with a successful page should not emit the dead-site notice")
	}
	if id, reason := batch.got(); id != source.Id || reason != interruptReason {
		t.Fatalf("record id=%s reason=%s", id, reason)
	}
	snap, ok := progress.Snapshot(source.Id)
	if !ok || snap.Status != progress.StatusRunning {
		t.Fatalf("status=%s, want running so written films can still seal", snap.Status)
	}
}

// 一上来连续 20 页都失败：中断，进度记失败，并发出站点失败通知。
func TestCollectPages_ZeroSuccessInterruptsAt20(t *testing.T) {
	shortenFailPause(t)
	env := setupFetchTest(t, func(int) bool { return true })
	var notified atomic.Int32
	var note string
	deps.OnSourceFailed = func(id, name, reason string) {
		notified.Add(1)
		note = reason
	}
	source := &model.FilmSource{Id: "streak_dead", Name: "死站"}
	batch := &errBatch{}
	progress.MarkSourcesCollectStarting([]model.FilmSource{*source}, "q-dead")

	has, succ, fail, failedPages, err := CollectPages(context.Background(), 100, nil, source, 24, batch)
	if has || succ != 0 || fail != 40 || len(failedPages) != 40 {
		t.Fatalf("has=%v succ=%d fail=%d failedPages=%d", has, succ, fail, len(failedPages))
	}
	if err == nil || err.Error() != interruptReason {
		t.Fatalf("err=%v", err)
	}
	pages, turns := env.snapshot()
	if uniquePages(pages) != 40 || turns != 120 {
		t.Fatalf("pages=%d turns=%d, want 40 pages and 120 requests", uniquePages(pages), turns)
	}
	if notified.Load() != 1 || note != interruptReason {
		t.Fatalf("notified=%d note=%s", notified.Load(), note)
	}
	if id, reason := batch.got(); id != source.Id || reason != interruptReason {
		t.Fatalf("record id=%s reason=%s", id, reason)
	}
	snap, ok := progress.Snapshot(source.Id)
	if !ok || snap.Status != progress.StatusFailed || snap.QueueId != "" || len(snap.FailedPages) != 40 {
		t.Fatalf("status=%s queue=%s failedPages=%d", snap.Status, snap.QueueId, len(snap.FailedPages))
	}
}

// 暂停的 2 分钟里点停止，这一页已入账，采集能返回，不把整站挂住。
func TestCollectPages_StopDuringPauseSettlesThePage(t *testing.T) {
	shortenFailPause(t)
	env := setupFetchTest(t, func(int) bool { return true })
	source := &model.FilmSource{Id: "pause_stop", Name: "暂停中停止"}
	progress.MarkSourcesCollectStarting([]model.FilmSource{*source}, "q-pause-stop")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = progress.WithRunEpoch(ctx, progress.Epoch(source.Id))
	type result struct {
		fail  int
		pages []int
	}
	done := make(chan result, 1)
	go func() {
		_, _, fail, failedPages, _ := CollectPages(ctx, 100, nil, source, 24, nil)
		done <- result{fail, failedPages}
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		snap, ok := progress.Snapshot(source.Id)
		if ok && snap.Status == progress.StatusPaused {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("source did not reach the rate-limit pause")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case got := <-done:
		if got.fail == 0 || len(got.pages) == 0 {
			requested, _ := env.snapshot()
			t.Fatalf("failed page was dropped, fail=%d recorded=%d requested=%d", got.fail, len(got.pages), uniquePages(requested))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stopping during the pause never returned")
	}
}

// 用户停止立刻返回，不写成连续失败中断。
func TestCollectPages_CancelReturnsWithoutInterrupt(t *testing.T) {
	oldDeps := deps
	oldWorkers := config.CollectPageWorkers
	t.Cleanup(func() {
		deps = oldDeps
		config.CollectPageWorkers = oldWorkers
	})
	config.CollectPageWorkers = 1

	started := make(chan struct{})
	var once sync.Once
	deps = Deps{
		WaitTurn: func(ctx context.Context, _ *model.FilmSource, tag string) (func(error), error) {
			if strings.Contains(tag, "pg=2 ") {
				once.Do(func() { close(started) })
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return func(error) {}, nil
		},
		GetFilmDetail: func(_ *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error) {
			pg, _ := strconv.Atoi(r.Params.Get("pg"))
			return []model.MovieDetail{{Id: int64(pg), Name: "片"}}, nil
		},
		SavePage: func(context.Context, *model.FilmSource, int, []model.MovieDetail) (scheduler.Mids, error) {
			return scheduler.Mids{}, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, _, _, err := CollectPages(ctx, 20, nil, &model.FilmSource{Id: "cancel_fetch", Name: "停止"}, 24, nil)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("page 2 did not reach the interval gate")
	}
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "中断") {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not return")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("cancel took %v", time.Since(start))
	}
}

// 失败页和入库失败都不截断列表。入库失败不计入连续抓取失败。
func TestCollectPages_RecordsAllFailedPages(t *testing.T) {
	env := setupFetchTest(t, func(pg int) bool { return pg%2 == 1 })
	source := &model.FilmSource{Id: "many_failed", Name: "大量失败"}
	progress.MarkSourcesCollectStarting([]model.FilmSource{*source}, "q-many-failed")

	_, succ, fail, failedPages, err := CollectPages(context.Background(), 1001, nil, source, 24, nil)
	if succ != 500 || fail != 501 || len(failedPages) != 501 {
		t.Fatalf("succ=%d fail=%d failedPages=%d, want 500/501/501", succ, fail, len(failedPages))
	}
	if err == nil || strings.Contains(err.Error(), "中断") {
		t.Fatalf("err=%v", err)
	}
	snap, ok := progress.Snapshot(source.Id)
	if !ok || len(snap.FailedPages) != 501 {
		t.Fatalf("progress failedPages=%d", len(snap.FailedPages))
	}
	pages, _ := env.snapshot()
	if uniquePages(pages) != 1001 {
		t.Fatalf("requested %d pages, want 1001", uniquePages(pages))
	}

	retryEnv := setupFetchTest(t, func(int) bool { return false })
	_, succ, fail, _, err = CollectPages(context.Background(), 1001, failedPages, source, 24, nil)
	retryPages, _ := retryEnv.snapshot()
	if err != nil || succ != 501 || fail != 0 || uniquePages(retryPages) != 501 {
		t.Fatalf("retry succ=%d fail=%d requested=%d err=%v", succ, fail, uniquePages(retryPages), err)
	}
}

func TestCollectPages_InFlightSuccessClearsStreak(t *testing.T) {
	oldDeps := deps
	oldWorkers := config.CollectPageWorkers
	t.Cleanup(func() {
		deps = oldDeps
		config.CollectPageWorkers = oldWorkers
	})
	config.CollectPageWorkers = 8

	source := &model.FilmSource{Id: "inflight_ok", Name: "在途成功"}
	progress.MarkSourcesCollectStarting([]model.FilmSource{*source}, "q-inflight")
	var passed atomic.Bool
	var pagesMu sync.Mutex
	var got []int
	deps = Deps{
		WaitTurn: func(context.Context, *model.FilmSource, string) (func(error), error) {
			return func(error) {}, nil
		},
		GetFilmDetail: func(_ *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error) {
			pg, _ := strconv.Atoi(r.Params.Get("pg"))
			pagesMu.Lock()
			got = append(got, pg)
			pagesMu.Unlock()
			if pg == 1 && !passed.Load() {
				deadline := time.Now().Add(2 * time.Second)
				for time.Now().Before(deadline) {
					if r.Ctx != nil && r.Ctx.Err() != nil {
						return nil, r.Ctx.Err()
					}
					snap, ok := progress.Snapshot(source.Id)
					if ok && snap.Failed >= 20 {
						passed.Store(true)
						return []model.MovieDetail{{Id: 1, Name: "片"}}, nil
					}
					time.Sleep(5 * time.Millisecond)
				}
				return nil, errors.New("slow page timed out")
			}
			if !passed.Load() {
				return nil, errors.New("down")
			}
			return []model.MovieDetail{{Id: int64(pg), Name: "片"}}, nil
		},
		SavePage: func(context.Context, *model.FilmSource, int, []model.MovieDetail) (scheduler.Mids, error) {
			return scheduler.Mids{}, nil
		},
	}

	_, succ, _, _, err := CollectPages(context.Background(), 40, nil, source, 24, nil)
	if err != nil && strings.Contains(err.Error(), "中断") {
		t.Fatalf("in-flight success should clear the streak, err=%v", err)
	}
	if succ == 0 {
		t.Fatal("slow page was cancelled")
	}
	pagesMu.Lock()
	n := uniquePages(got)
	pagesMu.Unlock()
	if n != 40 {
		t.Fatalf("requested %d pages, want 40", n)
	}
}

func TestCollectPages_WriteFailureDoesNotInterrupt(t *testing.T) {
	env := setupFetchTest(t, func(int) bool { return false })
	deps.SavePage = func(context.Context, *model.FilmSource, int, []model.MovieDetail) (scheduler.Mids, error) {
		return scheduler.Mids{}, errors.New("db down")
	}
	source := &model.FilmSource{Id: "write_fail", Name: "入库失败"}

	_, succ, fail, _, err := CollectPages(context.Background(), 25, nil, source, 24, nil)
	if succ != 0 || fail != 25 {
		t.Fatalf("succ=%d fail=%d, want 0/25", succ, fail)
	}
	if err == nil || strings.Contains(err.Error(), "中断") {
		t.Fatalf("err=%v", err)
	}
	pages, _ := env.snapshot()
	if uniquePages(pages) != 25 {
		t.Fatalf("requested %d pages, want 25", uniquePages(pages))
	}
}
