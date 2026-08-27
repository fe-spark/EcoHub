package notify

import "testing"

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
