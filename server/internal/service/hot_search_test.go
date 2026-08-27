package service

import (
	"strings"
	"testing"

	"server/internal/model/dto"
)

func TestClampHotSearchLimit(t *testing.T) {
	if got := clampHotSearchLimit(0); got != hotSearchDefaultLimit {
		t.Fatalf("zero: want %d got %d", hotSearchDefaultLimit, got)
	}
	if got := clampHotSearchLimit(-3); got != hotSearchDefaultLimit {
		t.Fatalf("neg: want %d got %d", hotSearchDefaultLimit, got)
	}
	if got := clampHotSearchLimit(8); got != 8 {
		t.Fatalf("default: want 8 got %d", got)
	}
	if got := clampHotSearchLimit(20); got != 20 {
		t.Fatalf("max: want 20 got %d", got)
	}
	if got := clampHotSearchLimit(99999); got != hotSearchMaxLimit {
		t.Fatalf("over: want %d got %d", hotSearchMaxLimit, got)
	}
}

func TestTruncateHotSearchKeyword(t *testing.T) {
	if got := truncateHotSearchKeyword("  庆余年  "); got != "庆余年" {
		t.Fatalf("trim: %q", got)
	}
	if got := truncateHotSearchKeyword(""); got != "" {
		t.Fatalf("empty: %q", got)
	}
	long := strings.Repeat("测", hotSearchKeywordMaxRunes+10)
	got := truncateHotSearchKeyword(long)
	if strings.Count(got, "测") != hotSearchKeywordMaxRunes {
		t.Fatalf("rune cap: len=%d want=%d", strings.Count(got, "测"), hotSearchKeywordMaxRunes)
	}
}

func TestShouldTrackHotSearch(t *testing.T) {
	if shouldTrackHotSearch(nil, "庆余年") {
		t.Fatal("nil page should not track")
	}
	if shouldTrackHotSearch(&dto.Page{Current: 1, Total: 3}, "  ") {
		t.Fatal("blank keyword should not track")
	}
	if shouldTrackHotSearch(&dto.Page{Current: 1, Total: 0}, "庆余年") {
		t.Fatal("empty result should not track")
	}
	if shouldTrackHotSearch(&dto.Page{Current: 2, Total: 30}, "庆余年") {
		t.Fatal("page 2 should not track")
	}
	if !shouldTrackHotSearch(&dto.Page{Current: 1, Total: 10}, "庆余年") {
		t.Fatal("first page with hits should track")
	}
	if !shouldTrackHotSearch(&dto.Page{Current: 0, Total: 10}, "庆余年") {
		t.Fatal("current<=1 with hits should track")
	}
}
