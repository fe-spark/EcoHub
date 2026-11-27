package fetcher

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"server/internal/collect/scheduler"
	"server/internal/model"
	"server/internal/utils"
)

func TestCollectPages_RejectsExcessivePageCount(t *testing.T) {
	_, _, _, _, err := CollectPages(context.Background(), MaxCollectPageCount+1, nil, &model.FilmSource{Id: "s_oom", Name: "OOM"}, 24, nil)
	if err == nil {
		t.Fatal("CollectPages 应拒绝超出 MaxCollectPageCount 的总页数")
	}
}

func TestCollectPages_StopsEarlyOnTrailingEmptyPagesAfterData(t *testing.T) {
	oldDeps := deps
	t.Cleanup(func() { deps = oldDeps })

	deps.WaitTurn = func(ctx context.Context, _ *model.FilmSource, _ string) (func(error), error) {
		return func(error) {}, nil
	}
	var fetched atomic.Int64
	deps.GetFilmDetail = func(s *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error) {
		fetched.Add(1)
		if r.Params.Get("pg") == "1" {
			return []model.MovieDetail{{Id: 1, Name: "正常影片"}}, nil
		}
		return nil, nil
	}
	deps.SavePage = func(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) (scheduler.Mids, error) {
		return scheduler.Mids{}, nil
	}

	_, _, _, _, err := CollectPages(context.Background(), 5000, nil, &model.FilmSource{Id: "s_empty_tail", Name: "EmptyTail"}, 24, nil)
	if err != nil {
		t.Fatalf("有数据后的尾页连续空页提前收尾不应报错，实际=%v", err)
	}
	if n := fetched.Load(); n >= 500 {
		t.Fatalf("连续空页应提前停止，实际抓取页数=%d", n)
	}
}

func TestCollectPages_AbortsWithErrorWhenAllPagesEmpty(t *testing.T) {
	oldDeps := deps
	t.Cleanup(func() { deps = oldDeps })

	deps.WaitTurn = func(ctx context.Context, _ *model.FilmSource, _ string) (func(error), error) {
		return func(error) {}, nil
	}
	var fetched atomic.Int64
	deps.GetFilmDetail = func(s *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error) {
		fetched.Add(1)
		return nil, nil
	}
	deps.SavePage = func(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) (scheduler.Mids, error) {
		return scheduler.Mids{}, nil
	}

	_, _, _, _, err := CollectPages(context.Background(), 5000, nil, &model.FilmSource{Id: "s_empty_all", Name: "EmptyAll"}, 24, nil)
	if err == nil {
		t.Fatal("从第 1 页起全空截断应返回错误，避免推进定时采集锚点")
	}
	if n := fetched.Load(); n >= 500 {
		t.Fatalf("全空截断应提前停止，实际抓取页数=%d", n)
	}
}

func TestCollectPages_SlowNonEmptyPagePreventsFalseEmptyTruncate(t *testing.T) {
	oldDeps := deps
	t.Cleanup(func() { deps = oldDeps })

	deps.WaitTurn = func(ctx context.Context, _ *model.FilmSource, _ string) (func(error), error) {
		return func(error) {}, nil
	}
	// 第 1 页慢返回非空，第 2..50 页共 49 页空，第 51 页慢返回非空，第 52 页先返回空，
	// 合计先完成 50 次空响应，但页码并不连续，不应截断第 53..55 页。
	var emptyDone atomic.Int32
	releaseSlow := make(chan struct{})
	var releaseOnce sync.Once
	var seenMu sync.Mutex
	seen := make(map[int]bool)

	deps.GetFilmDetail = func(s *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error) {
		pg, _ := strconv.Atoi(r.Params.Get("pg"))
		seenMu.Lock()
		seen[pg] = true
		seenMu.Unlock()

		switch {
		case pg == 1 || pg == 51:
			select {
			case <-releaseSlow:
			case <-time.After(2 * time.Second):
			}
			return []model.MovieDetail{{Id: int64(pg), Name: "慢页影片"}}, nil
		case pg >= 53:
			return []model.MovieDetail{{Id: int64(pg), Name: "后续页影片"}}, nil
		default:
			if emptyDone.Add(1) >= consecutiveEmptyPageLimit {
				releaseOnce.Do(func() { close(releaseSlow) })
			}
			return nil, nil
		}
	}
	deps.SavePage = func(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) (scheduler.Mids, error) {
		return scheduler.Mids{}, nil
	}

	_, succ, fail, _, err := CollectPages(context.Background(), 55, nil, &model.FilmSource{Id: "s_slow_nonempty", Name: "SlowNonEmpty"}, 24, nil)
	if err != nil {
		t.Fatalf("非连续空页不应中断，实际错误=%v", err)
	}
	if fail != 0 || succ != 55 {
		t.Fatalf("期望全部 55 页成功，实际 succ=%d fail=%d", succ, fail)
	}
	for pg := 53; pg <= 55; pg++ {
		if !seen[pg] {
			t.Fatalf("后续第 %d 页被误截断", pg)
		}
	}
}
