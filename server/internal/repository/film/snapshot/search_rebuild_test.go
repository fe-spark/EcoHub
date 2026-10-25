package snapshot

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func resetSearchRebuildForTest(t *testing.T, debounce time.Duration, scan func() error) {
	t.Helper()
	prevDebounce := searchRebuildDebounce
	prevScan := scanActiveFilmSearchIndex
	searchRebuildDebounce = debounce
	scanActiveFilmSearchIndex = scan
	t.Cleanup(func() {
		searchRebuild.mu.Lock()
		if searchRebuild.timer != nil {
			searchRebuild.timer.Stop()
			searchRebuild.timer = nil
		}
		searchRebuild.gen = 0
		searchRebuild.running = false
		searchRebuild.mu.Unlock()
		searchRebuildDebounce = prevDebounce
		scanActiveFilmSearchIndex = prevScan
	})
}

func waitScans(t *testing.T, scans *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if scans.Load() >= want {
			searchRebuild.mu.Lock()
			running := searchRebuild.running
			searchRebuild.mu.Unlock()
			if scans.Load() == want && !running {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	searchRebuild.mu.Lock()
	running := searchRebuild.running
	searchRebuild.mu.Unlock()
	t.Fatalf("scans=%d running=%v", scans.Load(), running)
}

func TestSearchRebuildCollapsesBurstIntoOneScan(t *testing.T) {
	var scans atomic.Int32
	resetSearchRebuildForTest(t, 30*time.Millisecond, func() error {
		scans.Add(1)
		return nil
	})
	RequestActiveFilmSearchIndexRebuild()
	RequestActiveFilmSearchIndexRebuild()
	RequestActiveFilmSearchIndexRebuild()
	waitScans(t, &scans, 1)
	time.Sleep(80 * time.Millisecond)
	if got := scans.Load(); got != 1 {
		t.Fatalf("burst started %d scans", got)
	}
}

func TestSearchRebuildRescansImmediatelyWhenGenMovesDuringScan(t *testing.T) {
	var scans atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	resetSearchRebuildForTest(t, 30*time.Millisecond, func() error {
		n := scans.Add(1)
		if n == 1 {
			once.Do(func() { close(started) })
			<-release
		}
		return nil
	})
	RequestActiveFilmSearchIndexRebuild()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first scan did not start")
	}
	RequestActiveFilmSearchIndexRebuild()
	closed := time.Now()
	close(release)
	waitScans(t, &scans, 2)
	if waited := time.Since(closed); waited > 80*time.Millisecond {
		t.Fatalf("rescan waited %s, want immediate", waited)
	}
}
