package notify

import (
	"database/sql"
	"testing"
)

func TestDailyPidFilter(t *testing.T) {
	if dailyPidFilter(DailyPidAll) != nil {
		t.Fatal("all should be nil filter")
	}
	other := dailyPidFilter(DailyPidOther)
	if other == nil || other.CategoryName != "其他" {
		t.Fatalf("other: %+v", other)
	}
	cat := dailyPidFilter(12)
	if cat == nil || cat.CategoryID != 12 {
		t.Fatalf("pid 12: %+v", cat)
	}
}

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

func TestDailyUpdateBaseQueryAllSkipsJoin(t *testing.T) {
	if dailyPidFilter(DailyPidAll) != nil {
		t.Fatal("pid=0 must skip nav filter (no film_index join needed)")
	}
	if dailyPidFilter(DailyPidOther) == nil || dailyPidFilter(12) == nil {
		t.Fatal("pid filter/-1/>0 still need join")
	}
}

func TestAccumulateDailyPidCountsIncludesNullPidInOther(t *testing.T) {
	navSet := map[int64]struct{}{1: {}, 2: {}}
	rows := []dailyPidCountRow{
		{Pid: sql.NullInt64{Int64: 1, Valid: true}, Count: 10},
		{Pid: sql.NullInt64{Int64: 9, Valid: true}, Count: 3},
		{Pid: sql.NullInt64{Valid: false}, Count: 2},
		{Pid: sql.NullInt64{Int64: 0, Valid: true}, Count: 1},
	}
	byPid, other, total := accumulateDailyPidCounts(rows, navSet)
	if total != 16 {
		t.Fatalf("total: want 16 got %d", total)
	}
	if byPid[1] != 10 {
		t.Fatalf("pid 1: want 10 got %d", byPid[1])
	}
	if other != 6 {
		t.Fatalf("other should include non-nav + NULL + pid0, got %d", other)
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
