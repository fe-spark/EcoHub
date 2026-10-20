package film

import (
	"testing"
)

func TestClampDailyUpdatePage(t *testing.T) {
	c, s := clampDailyUpdatePage(0, 0)
	if c != 1 || s != 21 {
		t.Fatalf("default: current=%d size=%d", c, s)
	}
	c, s = clampDailyUpdatePage(3, 200)
	if c != 3 || s != 100 {
		t.Fatalf("clamp size: current=%d size=%d", c, s)
	}
}

func TestClampDailyUpdateExclude(t *testing.T) {
	if ClampDailyUpdateExclude(nil, 500) != nil {
		t.Fatal("nil stays nil")
	}
	in := []int64{1, 2, 3}
	got := ClampDailyUpdateExclude(in, 500)
	if len(got) != 3 {
		t.Fatalf("under cap: %v", got)
	}
	got = ClampDailyUpdateExclude(in, 2)
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("truncate: %v", got)
	}
}

func TestPickRandomDailyUpdateRows(t *testing.T) {
	if got := pickRandomDailyUpdateRows(nil, 21); len(got) != 0 {
		t.Fatalf("nil: %+v", got)
	}
	pool := []dailyUpdateMidRow{{Mid: 1}, {Mid: 2}, {Mid: 3}, {Mid: 4}}
	got := pickRandomDailyUpdateRows(pool, 2)
	if len(got) != 2 {
		t.Fatalf("pageSize 2: %+v", got)
	}
	seen := map[int64]struct{}{}
	for _, r := range pool {
		seen[r.Mid] = struct{}{}
	}
	for _, r := range got {
		if _, ok := seen[r.Mid]; !ok {
			t.Fatalf("unexpected mid %d", r.Mid)
		}
	}
	got = pickRandomDailyUpdateRows(pool, 21)
	if len(got) != 4 {
		t.Fatalf("under pageSize should keep all, got %d", len(got))
	}
}
