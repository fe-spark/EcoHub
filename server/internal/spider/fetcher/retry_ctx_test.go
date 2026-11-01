package fetcher

import (
	"context"
	"net/url"
	"testing"
	"time"

	"server/internal/model"
	"server/internal/utils"
)

func TestGetFilmDetailWithRetry_WaitTurnIgnoresPageTimeout(t *testing.T) {
	old := deps
	t.Cleanup(func() { deps = old })

	parent, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps.WaitTurn = func(ctx context.Context, _ *model.FilmSource, _ string) (func(error), error) {
		if _, ok := ctx.Deadline(); ok {
			t.Error("等令牌不应带上单页 15 秒超时")
		}
		cancel()
		return nil, ctx.Err()
	}
	deps.GetFilmDetail = func(*model.FilmSource, utils.RequestInfo) ([]model.MovieDetail, error) {
		t.Fatal("限流等待失败后不应发请求")
		return nil, nil
	}

	_, err := GetFilmDetailWithRetry(parent, &model.FilmSource{Id: "s"}, utils.RequestInfo{Params: url.Values{}})
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestGetFilmDetailWithRetry_HTTPUsesPageTimeout(t *testing.T) {
	old := deps
	t.Cleanup(func() { deps = old })

	deps.WaitTurn = func(ctx context.Context, _ *model.FilmSource, _ string) (func(error), error) {
		if _, ok := ctx.Deadline(); ok {
			t.Error("等令牌不应带上单页 15 秒超时")
		}
		return func(error) {}, nil
	}
	deps.GetFilmDetail = func(_ *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error) {
		deadline, ok := r.Ctx.Deadline()
		if !ok {
			t.Fatal("单页请求应有超时")
		}
		left := time.Until(deadline)
		if left <= 0 || left > singlePageFetchTimeout+time.Second {
			t.Fatalf("page timeout left=%s", left)
		}
		return []model.MovieDetail{{Id: 1, Name: "甲"}}, nil
	}

	list, err := GetFilmDetailWithRetry(context.Background(), &model.FilmSource{Id: "s"}, utils.RequestInfo{Params: url.Values{}})
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
}
