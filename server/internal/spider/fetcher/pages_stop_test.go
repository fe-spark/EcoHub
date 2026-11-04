package fetcher

import "testing"

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
