package fetcher

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"server/internal/model"
	"server/internal/spider/scheduler"
	"server/internal/utils"
)

func TestShouldStopUnprovenSource(t *testing.T) {
	if shouldStopUnprovenSource(0, sourceStartupFailLimit-1) {
		t.Fatal("zero success below the limit should keep going")
	}
	if !shouldStopUnprovenSource(0, sourceStartupFailLimit) {
		t.Fatal("zero success at the limit should stop")
	}
	if shouldStopUnprovenSource(1, sourceStartupFailLimit*10) {
		t.Fatal("any success should keep the rest of the pages")
	}
	if shouldStopUnprovenSource(2641, 10) {
		t.Fatal("a short failure streak after many successes should not stop")
	}
}

func TestCollectPages_ZeroSuccessStopAtLimit(t *testing.T) {
	oldDeps := deps
	t.Cleanup(func() { deps = oldDeps })

	var failCalls atomic.Int32
	deps.WaitTurn = func(ctx context.Context, _ *model.FilmSource, _ string) (func(error), error) {
		return func(error) {}, nil
	}
	deps.GetFilmDetail = func(s *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error) {
		failCalls.Add(1)
		return nil, errors.New("500 internal server error")
	}
	deps.SavePage = func(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) (scheduler.Mids, error) {
		return scheduler.Mids{}, nil
	}

	source := &model.FilmSource{Id: "test_unproven", Name: "死站"}
	hasWrites, succ, fail, err := CollectPages(context.Background(), 100, source, 24, nil)
	if hasWrites {
		t.Fatal("hasWrites should be false")
	}
	if succ != 0 {
		t.Fatalf("succ=%d want 0", succ)
	}
	if fail < sourceStartupFailLimit {
		t.Fatalf("fail=%d want >= %d", fail, sourceStartupFailLimit)
	}
	if err == nil || !strings.Contains(err.Error(), "尚无成功页") {
		t.Fatalf("expected 零成功停站 error, got: %v", err)
	}
}
