package snapshot

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func resetSearchRebuildForTest(t *testing.T, debounce time.Duration, scan func() (int64, error)) {
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
		NotifySearchIndexProgress(0, 0, "idle")
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
	resetSearchRebuildForTest(t, 30*time.Millisecond, func() (int64, error) {
		scans.Add(1)
		return 0, nil
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
	resetSearchRebuildForTest(t, 30*time.Millisecond, func() (int64, error) {
		n := scans.Add(1)
		if n == 1 {
			once.Do(func() { close(started) })
			<-release
		}
		return 0, nil
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

func TestNotifySearchIndexIfCurrentSkipsStaleGen(t *testing.T) {
	resetSearchRebuildForTest(t, time.Hour, func() (int64, error) { return 0, nil })
	NotifySearchIndexProgress(10, 1, "indexing")
	searchRebuild.mu.Lock()
	searchRebuild.gen = 2
	searchRebuild.mu.Unlock()

	notifySearchIndexIfCurrent(1, 0, 0, "idle")
	if !IsSearchIndexBuilding() {
		t.Fatal("stale idle cleared the build flag")
	}
	if got := GetLatestSearchIndexProgress().Status; got != "indexing" {
		t.Fatalf("status=%s", got)
	}

	notifySearchIndexIfCurrent(2, 10, 10, "done")
	if IsSearchIndexBuilding() {
		t.Fatal("current done left the build flag set")
	}
	got := GetLatestSearchIndexProgress()
	if got.Status != "done" || got.Current != 10 {
		t.Fatalf("progress=%+v", got)
	}
}

func TestSearchRebuildFailureKeepsNewerIndexing(t *testing.T) {
	var scans atomic.Int32
	resetSearchRebuildForTest(t, 20*time.Millisecond, func() (int64, error) {
		n := scans.Add(1)
		if n == 1 {
			RequestActiveFilmSearchIndexRebuild()
			return 0, errors.New("scan failed")
		}
		return 3, nil
	})
	RequestActiveFilmSearchIndexRebuild()
	waitScans(t, &scans, 2)
	if IsSearchIndexBuilding() {
		t.Fatal("newer scan did not finish")
	}
	if got := GetLatestSearchIndexProgress().Status; got != "done" {
		t.Fatalf("status=%s", got)
	}
}

func TestBuildAndStoreClearsSearchIndexBuilding(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	createTestFilm(t, gdb, 501, "索引影片", 1000, "默认主源")
	t.Cleanup(func() {
		NotifySearchIndexProgress(0, 0, "idle")
	})

	idx, err := buildAndStoreFilmSearchMetaIndex("v-build-clear")
	if err != nil {
		t.Fatal(err)
	}
	if idx == nil || len(idx.Items) != 1 {
		t.Fatalf("index items=%v", idx)
	}
	if IsSearchIndexBuilding() {
		t.Fatal("full load left isSearchIndexBuilding set")
	}
	got := GetLatestSearchIndexProgress()
	if got.Status != "done" || got.Total != 1 || got.Current != 1 {
		t.Fatalf("progress=%+v", got)
	}
}

// 监听方收到重建终态时，running 必须已经清掉。
// 否则封口队列会按残留的 running 判断「重建还在跑」而停下，之后再也等不到终态。
var rebuildDoneSawRunning atomic.Int32 // 0 未观察，1 已清，2 仍在跑
var watchRebuildDone atomic.Bool
var rebuildDoneListener sync.Once

func TestSearchRebuildClearsRunningBeforeDone(t *testing.T) {
	rebuildDoneListener.Do(func() {
		AddSearchIndexProgressListener(func(p SearchIndexProgress) {
			// 终态在重建锁里发，这里直接读 running。
			if watchRebuildDone.Load() && p.Status == "done" {
				if searchRebuild.running {
					rebuildDoneSawRunning.Store(2)
				} else {
					rebuildDoneSawRunning.Store(1)
				}
			}
		})
	})
	var scans atomic.Int32
	resetSearchRebuildForTest(t, 10*time.Millisecond, func() (int64, error) {
		scans.Add(1)
		return 5, nil
	})
	rebuildDoneSawRunning.Store(0)
	watchRebuildDone.Store(true)
	t.Cleanup(func() { watchRebuildDone.Store(false) })

	RequestActiveFilmSearchIndexRebuild()
	waitScans(t, &scans, 1)
	switch rebuildDoneSawRunning.Load() {
	case 0:
		t.Fatal("没有收到重建终态")
	case 2:
		t.Fatal("发 done 时 running 还没清")
	}
	if got := GetLatestSearchIndexProgress(); got.Status != "done" || got.Total != 5 {
		t.Fatalf("progress=%+v", got)
	}
}
