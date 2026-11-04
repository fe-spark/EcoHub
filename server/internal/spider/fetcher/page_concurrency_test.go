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

	_, err := GetFilmDetailWithRetry(context.Background(), &model.FilmSource{Id: "s"}, utils.RequestInfo{Params: url.Values{}})
	if err == nil || err.Error() != "response list is empty" {
		t.Fatalf("err=%v", err)
	}
	if calls != 1 {
		t.Fatalf("empty list calls=%d", calls)
	}
}

func TestGetFilmDetailWithRetry_RateLimitRetriesOnce(t *testing.T) {
	old := deps
	t.Cleanup(func() { deps = old })

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
