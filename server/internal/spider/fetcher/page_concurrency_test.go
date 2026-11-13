package fetcher

import (
	"context"
	"errors"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"server/internal/config"
	"server/internal/model"
	"server/internal/utils"
)

func TestFetchSlotBlocksWhenFull(t *testing.T) {
	slots := newFetchGate(func() int { return 1 })
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

func TestFetchSlotFollowsRaisedLimit(t *testing.T) {
	limit := 1
	slots := newFetchGate(func() int { return limit })
	if err := acquireSlot(context.Background(), slots); err != nil {
		t.Fatal(err)
	}
	got := make(chan error, 1)
	go func() {
		got <- acquireSlot(context.Background(), slots)
	}()
	select {
	case err := <-got:
		if err != nil {
			t.Fatal(err)
		}
		t.Fatal("limit 1 should block the second acquire")
	case <-time.After(30 * time.Millisecond):
	}
	limit = 2
	select {
	case err := <-got:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("raised limit did not wake the waiter")
	}
	releaseSlot(slots)
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
	t.Cleanup(func() { deps = old })

	var calls, turns int
	deps.WaitTurn = func(context.Context, *model.FilmSource, string) (func(error), error) {
		turns++
		return func(error) {}, nil
	}
	deps.GetFilmDetail = func(*model.FilmSource, utils.RequestInfo) ([]model.MovieDetail, error) {
		calls++
		if calls < 3 {
			return nil, errors.New("HTTP 429 Too Many Requests")
		}
		return []model.MovieDetail{{Id: 1, Name: "甲"}}, nil
	}

	list, err := GetFilmDetailWithRetry(context.Background(), &model.FilmSource{Id: "s"}, utils.RequestInfo{Params: url.Values{}})
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if calls != 3 || turns != 3 {
		t.Fatalf("429 retries twice through the interval gate, calls=%d turns=%d", calls, turns)
	}
}

func TestGetFilmDetailWithRetry_AnyErrorRetriesOnce(t *testing.T) {
	old := deps
	t.Cleanup(func() { deps = old })

	var calls int
	deps.WaitTurn = func(context.Context, *model.FilmSource, string) (func(error), error) {
		return func(error) {}, nil
	}
	deps.GetFilmDetail = func(*model.FilmSource, utils.RequestInfo) ([]model.MovieDetail, error) {
		calls++
		if calls < 3 {
			return nil, errors.New("invalid character '<' looking for beginning of value")
		}
		return nil, errors.New("still broken")
	}

	_, err := GetFilmDetailWithRetry(context.Background(), &model.FilmSource{Id: "s"}, utils.RequestInfo{Params: url.Values{}})
	if err == nil || err.Error() != "still broken" {
		t.Fatalf("err=%v", err)
	}
	if calls != 3 {
		t.Fatalf("any error retries twice, calls=%d", calls)
	}
}

func TestGetFilmDetailWithRetry_ParentCancelSkipsRetry(t *testing.T) {
	old := deps
	t.Cleanup(func() { deps = old })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var turns int
	deps.WaitTurn = func(ctx context.Context, _ *model.FilmSource, _ string) (func(error), error) {
		turns++
		return nil, ctx.Err()
	}
	deps.GetFilmDetail = func(*model.FilmSource, utils.RequestInfo) ([]model.MovieDetail, error) {
		t.Fatal("cancelled parent should not fetch")
		return nil, nil
	}

	_, err := GetFilmDetailWithRetry(ctx, &model.FilmSource{Id: "s"}, utils.RequestInfo{Params: url.Values{}})
	if err == nil {
		t.Fatal("expected context error")
	}
	if turns != 1 {
		t.Fatalf("turns=%d want 1", turns)
	}
}

func TestGetPageCountWithRetry_RetriesOnce(t *testing.T) {
	old := deps
	t.Cleanup(func() { deps = old })

	var calls int
	deps.WaitTurn = func(context.Context, *model.FilmSource, string) (func(error), error) {
		return func(error) {}, nil
	}
	deps.GetPageCount = func(*model.FilmSource, utils.RequestInfo) (int, error) {
		calls++
		if calls == 1 {
			return 0, errors.New("eof")
		}
		return 12, nil
	}

	n, err := GetPageCountWithRetry(context.Background(), &model.FilmSource{Id: "s"}, utils.RequestInfo{Params: url.Values{}})
	if err != nil || n != 12 || calls != 2 {
		t.Fatalf("n=%d calls=%d err=%v", n, calls, err)
	}
}

func TestWithFetchSlot_ClaimsAfterSlot(t *testing.T) {
	old := deps
	oldFetch := config.CollectFetchInFlight
	t.Cleanup(func() {
		deps = old
		config.CollectFetchInFlight = oldFetch
	})
	config.CollectFetchInFlight = 1
	if config.FetchInFlightLimit() != 1 {
		t.Fatal("slot limit is not 1")
	}

	var mu sync.Mutex
	var next time.Time
	var claims atomic.Int32
	startedHTTP := make(chan struct{})
	releaseHTTP := make(chan struct{})
	var once sync.Once
	deps = Deps{
		WaitTurn: func(ctx context.Context, _ *model.FilmSource, _ string) (func(error), error) {
			mu.Lock()
			wait := time.Until(next)
			mu.Unlock()
			if wait > 0 {
				timer := time.NewTimer(wait)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil, ctx.Err()
				case <-timer.C:
				}
			}
			return func(error) {}, nil
		},
		ClaimTurn: func(*model.FilmSource) bool {
			mu.Lock()
			defer mu.Unlock()
			if !next.IsZero() && next.After(time.Now()) {
				return false
			}
			next = time.Now().Add(150 * time.Millisecond)
			claims.Add(1)
			return true
		},
	}

	if err := acquireSlot(context.Background(), globalFetchSlots()); err != nil {
		t.Fatal(err)
	}
	source := &model.FilmSource{Id: "slot-claim", CollectIntervalMs: 150}
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			done <- withFetchSlot(context.Background(), source, "page", func(context.Context) error {
				once.Do(func() { close(startedHTTP) })
				<-releaseHTTP
				return nil
			})
		}()
	}
	time.Sleep(40 * time.Millisecond)
	if claims.Load() != 0 {
		t.Fatalf("claimed %d while waiting for a slot", claims.Load())
	}
	releaseSlot(globalFetchSlots())

	select {
	case <-startedHTTP:
	case <-time.After(time.Second):
		t.Fatal("first request did not start")
	}
	if claims.Load() != 1 {
		t.Fatalf("claims=%d want 1 while the first request holds the slot", claims.Load())
	}
	close(releaseHTTP)
	timeout := time.After(time.Second)
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-timeout:
			t.Fatal("requests did not finish")
		}
	}
	if claims.Load() != 2 {
		t.Fatalf("claims=%d want 2", claims.Load())
	}
}
