package service

import "testing"

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
