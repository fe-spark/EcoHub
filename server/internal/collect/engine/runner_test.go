package engine

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"server/internal/collect/progress"
	"server/internal/config"
	"server/internal/model"
	"server/internal/utils"
)

type stubCollector struct {
	getDetail func(r utils.RequestInfo) ([]model.MovieDetail, error)
}

func (c *stubCollector) GetCategoryTree(utils.RequestInfo) (*model.CategoryTree, int, error) {
	return &model.CategoryTree{}, 0, nil
}
func (c *stubCollector) ProbePage1(utils.RequestInfo) (int, []model.MovieDetail, error) {
	return 0, nil, nil
}
func (c *stubCollector) GetPageCount(utils.RequestInfo) (int, error) { return 0, nil }
func (c *stubCollector) GetFilmDetail(r utils.RequestInfo) ([]model.MovieDetail, error) {
	return c.getDetail(r)
}

type mockBatch struct {
	mu      sync.Mutex
	errID   string
	errMsg  string
	mids    []int64
	skipped int
}

func (b *mockBatch) IsStandalone() bool { return false }
func (b *mockBatch) AddAffectedMIDs(_ *model.FilmSource, _ int, mids []int64) {
	b.mu.Lock()
	b.mids = append(b.mids, mids...)
	b.mu.Unlock()
}
func (b *mockBatch) RecordSourceError(sourceID, reason string) {
	b.mu.Lock()
	b.errID = sourceID
	b.errMsg = reason
	b.mu.Unlock()
}
func (b *mockBatch) AddCategorySkip(_ string, n int) {
	b.mu.Lock()
	b.skipped += n
	b.mu.Unlock()
}

func shortenFailPause(t *testing.T) {
	t.Helper()
	prev := fetchFailPause
	fetchFailPause = 20 * time.Millisecond
	t.Cleanup(func() { fetchFailPause = prev })
}

func newTestRunner(getDetail func(pg int) ([]model.MovieDetail, error), save func(pg int, list []model.MovieDetail) (model.Mids, error)) *Runner {
	return NewRunner(Runner{
		ResolveCollector: func(string) Collector {
			return &stubCollector{getDetail: func(r utils.RequestInfo) ([]model.MovieDetail, error) {
				pg, _ := strconv.Atoi(r.Params.Get("pg"))
				return getDetail(pg)
			}}
		},
		SavePage: func(_ context.Context, _ *model.FilmSource, page int, list []model.MovieDetail) (model.Mids, error) {
			if save != nil {
				return save(page, list)
			}
			return model.Mids{}, nil
		},
		Throttle:  NewThrottleStore(func() int { return 16 }, func() int { return 8 }),
		WritePool: NewWritePool(func() int { return 8 }),
	})
}

func TestCollectPages_ReusesPage1SeedWithoutDuplicateFetch(t *testing.T) {
	var fetchedMu sync.Mutex
	var fetched []int
	var saved []int
	runner := newTestRunner(
		func(pg int) ([]model.MovieDetail, error) {
			fetchedMu.Lock()
			fetched = append(fetched, pg)
			fetchedMu.Unlock()
			return []model.MovieDetail{{Id: int64(pg), Name: "片"}}, nil
		},
		func(pg int, list []model.MovieDetail) (model.Mids, error) {
			fetchedMu.Lock()
			saved = append(saved, pg)
			fetchedMu.Unlock()
			return model.Mids{Affected: []int64{int64(pg)}}, nil
		},
	)

	seed := &Page1Result{
		PageCount: 3,
		Items:     []model.MovieDetail{{Id: 1, Name: "第一页复用片"}},
	}
	batch := &mockBatch{}
	has, succ, fail, failedPages, err := runner.CollectPages(context.Background(), 3, nil, seed, &model.FilmSource{Id: "seed_src", Name: "Seed"}, 24, batch)
	if err != nil || !has || succ != 3 || fail != 0 || len(failedPages) != 0 {
		t.Fatalf("has=%v succ=%d fail=%d err=%v", has, succ, fail, err)
	}
	for _, pg := range fetched {
		if pg == 1 {
			t.Fatal("已注入 Page1Seed 后不应重复发 HTTP 请求抓取第 1 页")
		}
	}
	if len(saved) != 3 {
		t.Fatalf("期望 3 页全部落库, saved=%v", saved)
	}
}

func TestCollectPages_ParallelSameSourceWrites(t *testing.T) {
	var active atomic.Int32
	var maxActive atomic.Int32
	runner := newTestRunner(
		func(pg int) ([]model.MovieDetail, error) {
			return []model.MovieDetail{{Id: int64(pg), Name: "片"}}, nil
		},
		func(pg int, _ []model.MovieDetail) (model.Mids, error) {
			cur := active.Add(1)
			for {
				prev := maxActive.Load()
				if cur <= prev || maxActive.CompareAndSwap(prev, cur) {
					break
				}
			}
			time.Sleep(30 * time.Millisecond)
			active.Add(-1)
			return model.Mids{}, nil
		},
	)

	has, succ, fail, _, err := runner.CollectPages(context.Background(), 8, nil, nil, &model.FilmSource{Id: "same_src_parallel", Name: "同站并发写"}, 24, nil)
	if err != nil || !has || succ != 8 || fail != 0 {
		t.Fatalf("has=%v succ=%d fail=%d err=%v", has, succ, fail, err)
	}
	if maxActive.Load() < 2 {
		t.Fatalf("同站多页写入被串行化了, maxActive=%d", maxActive.Load())
	}
}

func TestCollectPages_WithTargetPagesAndEmptySuccessAndCancel(t *testing.T) {
	runner := newTestRunner(
		func(pg int) ([]model.MovieDetail, error) {
			if pg == 3190 {
				return nil, errors.New("timeout")
			}
			return []model.MovieDetail{{Id: int64(pg), Name: "测试"}}, nil
		},
		func(pg int, _ []model.MovieDetail) (model.Mids, error) {
			return model.Mids{Affected: []int64{int64(pg)}}, nil
		},
	)

	has, succ, fail, failedPages, err := runner.CollectPages(context.Background(), 10000, []int{3190, 4036}, nil, &model.FilmSource{Id: "targets", Name: "目标页"}, 24, nil)
	if !has || succ != 1 || fail != 1 || len(failedPages) != 1 || failedPages[0] != 3190 || err == nil {
		t.Fatalf("has=%v succ=%d fail=%d failedPages=%v err=%v", has, succ, fail, failedPages, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var saves atomic.Int32
	cancelRunner := newTestRunner(
		func(pg int) ([]model.MovieDetail, error) {
			cancel()
			return []model.MovieDetail{{Id: 101, Name: "取消"}}, nil
		},
		func(int, []model.MovieDetail) (model.Mids, error) {
			saves.Add(1)
			return model.Mids{}, nil
		},
	)
	has, succ, _, _, err = cancelRunner.CollectPages(ctx, 1, nil, nil, &model.FilmSource{Id: "cancel_src", Name: "取消"}, 24, nil)
	if !errors.Is(err, context.Canceled) || has || succ != 0 || saves.Load() != 0 {
		t.Fatalf("canceled collect should drop save: has=%v succ=%d saves=%d err=%v", has, succ, saves.Load(), err)
	}
}

func TestCollectPages_EmptyTailAndFailStreak(t *testing.T) {
	shortenFailPause(t)
	prevWorkers := config.CollectPageWorkers
	config.CollectPageWorkers = 1
	t.Cleanup(func() { config.CollectPageWorkers = prevWorkers })

	var tailFetched atomic.Int64
	tailRunner := newTestRunner(func(pg int) ([]model.MovieDetail, error) {
		tailFetched.Add(1)
		if pg == 1 {
			return []model.MovieDetail{{Id: 1, Name: "正常片"}}, nil
		}
		return nil, nil
	}, nil)
	if _, _, _, _, err := tailRunner.CollectPages(context.Background(), 5000, nil, nil, &model.FilmSource{Id: "empty_tail", Name: "尾空"}, 24, nil); err != nil {
		t.Fatalf("trailing empty after data should stop cleanly: %v", err)
	}
	if tailFetched.Load() >= 500 {
		t.Fatalf("tail fetched too many pages: %d", tailFetched.Load())
	}

	allEmptyRunner := newTestRunner(func(int) ([]model.MovieDetail, error) { return nil, nil }, nil)
	if _, _, _, _, err := allEmptyRunner.CollectPages(context.Background(), 5000, nil, nil, &model.FilmSource{Id: "empty_all", Name: "全空"}, 24, nil); err == nil {
		t.Fatal("all empty pages should return error")
	}

	var notified atomic.Int32
	deadRunner := newTestRunner(func(int) ([]model.MovieDetail, error) { return nil, errors.New("bad gateway") }, nil)
	deadRunner.Throttle = NewThrottleStore(func() int { return 4 }, func() int { return 1 })
	deadRunner.OnSourceFailed = func(string, string, string) { notified.Add(1) }
	deadSrc := &model.FilmSource{Id: "dead_streak", Name: "连败"}
	progress.MarkSourcesCollectStarting([]model.FilmSource{*deadSrc}, "q-dead")
	batch := &mockBatch{}
	has, succ, fail, _, err := deadRunner.CollectPages(context.Background(), 100, nil, nil, deadSrc, 24, batch)
	if has || succ != 0 || fail != 40 || err == nil || !strings.Contains(err.Error(), "已中断采集") || notified.Load() != 1 {
		t.Fatalf("has=%v succ=%d fail=%d err=%v notified=%d", has, succ, fail, err, notified.Load())
	}
}

func TestCollectPages_RejectsExcessivePages(t *testing.T) {
	runner := newTestRunner(nil, nil)
	_, _, _, _, err := runner.CollectPages(context.Background(), MaxCollectPageCount+1, nil, nil, &model.FilmSource{Id: "oom", Name: "OOM"}, 24, nil)
	if err == nil {
		t.Fatal("should reject page count over MaxCollectPageCount")
	}
}

func TestFetchStreak_StateTransitions(t *testing.T) {
	st, cancel := newFetchStreak(context.Background())
	defer cancel()
	for i := 0; i < consecutiveFetchFailLimit-1; i++ {
		if !st.tryBegin() {
			t.Fatal("tryBegin false")
		}
		stop, pause := st.finish(false)
		if stop || pause {
			t.Fatalf("unexpected stop=%v pause=%v", stop, pause)
		}
	}
	if !st.tryBegin() {
		t.Fatal("tryBegin false at 20th")
	}
	stop, pause := st.finish(false)
	if stop || !pause {
		t.Fatalf("want pause at 20th failure, got stop=%v pause=%v", stop, pause)
	}
	st.resumeIntake()
	for i := 0; i < consecutiveFetchFailLimit-1; i++ {
		st.tryBegin()
		stop, _ = st.finish(false)
		if stop {
			t.Fatalf("stopped early at %d", i+1)
		}
	}
	st.tryBegin()
	stop, _ = st.finish(false)
	if !stop {
		t.Fatal("expected stop after 20 failures post-resume")
	}
}

func TestCollectPages_SeedPg1PreCanceledNoGoroutineLeak(t *testing.T) {
	runner := newTestRunner(nil, nil)
	seed := &Page1Result{
		PageCount: 2,
		Items:     []model.MovieDetail{{Id: 1, Name: "取消不泄露"}},
	}
	batch := &mockBatch{}

	before := runtime.NumGoroutine()
	for i := 0; i < 30; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		has, succ, _, _, err := runner.CollectPages(ctx, 2, nil, seed, &model.FilmSource{Id: "pre_cancel", Name: "预取消"}, 24, batch)
		if !errors.Is(err, context.Canceled) || has || succ != 0 {
			t.Fatalf("unexpected result on canceled run %d: has=%v succ=%d err=%v", i, has, succ, err)
		}
	}
	runtime.Gosched()
	after := runtime.NumGoroutine()
	if after-before >= 10 {
		t.Fatalf("suspected goroutine leak on pre-canceled seed run: before=%d after=%d", before, after)
	}
}
