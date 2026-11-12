package fetcher

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"server/internal/model"
	"server/internal/utils"
)

func TestFetchRetryDelay_SpacesTransientRetries(t *testing.T) {
	old := fetchRetryBase
	t.Cleanup(func() { fetchRetryBase = old })
	fetchRetryBase = time.Second
	if got := fetchRetryDelay(1); got != time.Second {
		t.Fatalf("first retry delay=%s", got)
	}
	if got := fetchRetryDelay(2); got != 4*time.Second {
		t.Fatalf("second retry delay=%s", got)
	}
	fetchRetryBase = 0
	if got := fetchRetryDelay(2); got != 0 {
		t.Fatalf("disabled delay=%s", got)
	}
}

func TestFetchSlotBlocksWhenFull(t *testing.T) {
	slots := make(chan struct{}, 1)
	if err := acquireSlot(context.Background(), slots); err != nil {
		t.Fatal(err)
	}
	blocked, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := acquireSlot(blocked, slots); err == nil {
		t.Fatal("full slot should block until the context ends")
	}
	releaseSlot(slots)
	if err := acquireSlot(context.Background(), slots); err != nil {
		t.Fatal(err)
	}
	releaseSlot(slots)
}

func TestGetFilmDetailWithRetry_EmptyListDoesNotRetry(t *testing.T) {
	old := deps
	t.Cleanup(func() { deps = old })

	var calls int
	deps.WaitTurn = func(context.Context, *model.FilmSource, string) (func(error), error) {
		return func(error) {}, nil
	}
	deps.GetFilmDetail = func(*model.FilmSource, utils.RequestInfo) ([]model.MovieDetail, error) {
		calls++
		return nil, nil
	}

	list, err := GetFilmDetailWithRetry(context.Background(), &model.FilmSource{Id: "s"}, utils.RequestInfo{Params: url.Values{}})
	if err != nil {
		t.Fatalf("empty list should succeed with nil err, got: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %d items", len(list))
	}
	if calls != 1 {
		t.Fatalf("empty list calls=%d", calls)
	}
}

func TestGetFilmDetailWithRetry_RateLimitRetriesOnce(t *testing.T) {
	old := deps
	oldDelay := fetchRetryBase
	t.Cleanup(func() {
		deps = old
		fetchRetryBase = oldDelay
	})
	fetchRetryBase = 0

	var calls int
	deps.WaitTurn = func(context.Context, *model.FilmSource, string) (func(error), error) {
		return func(error) {}, nil
	}
	deps.GetFilmDetail = func(*model.FilmSource, utils.RequestInfo) ([]model.MovieDetail, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("HTTP 429 Too Many Requests")
		}
		return []model.MovieDetail{{Id: 1, Name: "甲"}}, nil
	}

	list, err := GetFilmDetailWithRetry(context.Background(), &model.FilmSource{Id: "s"}, utils.RequestInfo{Params: url.Values{}})
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if calls != 2 {
		t.Fatalf("429 should retry once, calls=%d", calls)
	}
}

func TestGetFilmDetailWithRetry_TimeoutRetriesAndSucceeds(t *testing.T) {
	old := deps
	oldDelay := fetchRetryBase
	t.Cleanup(func() {
		deps = old
		fetchRetryBase = oldDelay
	})
	fetchRetryBase = 0

	var calls int
	deps.WaitTurn = func(context.Context, *model.FilmSource, string) (func(error), error) {
		return func(error) {}, nil
	}
	deps.GetFilmDetail = func(*model.FilmSource, utils.RequestInfo) ([]model.MovieDetail, error) {
		calls++
		if calls == 1 {
			return nil, errors.New(`Get "https://lovedan.net/api.php?pg=9408": context deadline exceeded`)
		}
		return []model.MovieDetail{{Id: 2, Name: "乙"}}, nil
	}

	list, err := GetFilmDetailWithRetry(context.Background(), &model.FilmSource{Id: "s"}, utils.RequestInfo{Params: url.Values{}})
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if calls != 2 {
		t.Fatalf("timeout should retry, calls=%d", calls)
	}
}
