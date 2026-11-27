package fetcher

import (
	"context"
	"sync/atomic"
	"testing"

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

func TestCollectPages_StopsEarlyOnConsecutiveEmptyPages(t *testing.T) {
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

	_, _, _, _, err := CollectPages(context.Background(), 5000, nil, &model.FilmSource{Id: "s_empty", Name: "Empty"}, 24, nil)
	if err != nil {
		t.Fatalf("连续空页提前收尾不应报错，实际=%v", err)
	}
	if n := fetched.Load(); n >= 500 {
		t.Fatalf("连续空页应提前停止，实际抓取页数=%d", n)
	}
}
