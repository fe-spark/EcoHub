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
