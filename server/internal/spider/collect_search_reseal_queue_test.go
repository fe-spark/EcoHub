package spider

import (
	"testing"
	"time"

	"server/internal/model"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider/progress"
)

func resetSearchReseal() {
	searchReseal.mu.Lock()
	searchReseal.pending = map[int64]struct{}{}
	searchReseal.sealing = map[string]struct{}{}
	searchReseal.running = false
	searchReseal.mu.Unlock()
	searchSealRunning.Store(false)
	filmsnapshot.NotifySearchIndexProgress(0, 0, "idle")
}

func TestSearchResealRun_WaitsThenSealsOnce(t *testing.T) {
	prevWait := waitCollectIdle
	prevApply := applySearchReseal
	t.Cleanup(func() {
		waitCollectIdle = prevWait
		applySearchReseal = prevApply
		collectLifecycle.endSource("busy-seal")
		resetSearchReseal()
	})
	if err := collectLifecycle.beginSource("busy-seal"); err != nil {
		t.Fatal(err)
	}

	waitStarted := make(chan struct{}, 1)
	release := make(chan struct{})
	waitCollectIdle = func() {
		select {
		case waitStarted <- struct{}{}:
		default:
		}
		<-release
	}
	sealed := make(chan int, 1)
	applySearchReseal = func(mids []int64) bool {
		sealed <- len(mids)
		return true
	}

	scheduleSearchReseal([]int64{1, 2, 1})
	scheduleSearchReseal([]int64{2, 3})

	if got := filmsnapshot.GetLatestSearchIndexProgress(); got.Status != "waiting" {
		t.Fatalf("status = %s, want waiting", got.Status)
	}

	select {
	case <-waitStarted:
	case <-time.After(time.Second):
		t.Fatal("seal did not wait")
	}
	select {
	case n := <-sealed:
		t.Fatalf("sealed while a source is collecting: %d", n)
	case <-time.After(40 * time.Millisecond):
	}

	collectLifecycle.endSource("busy-seal")
	close(release)
	select {
	case n := <-sealed:
		if n != 3 {
			t.Fatalf("sealed %d mids, want 3", n)
		}
	case <-time.After(time.Second):
		t.Fatal("did not seal after sources went idle")
	}
}

func TestSearchResealRun_CompletesEmptyCycle(t *testing.T) {
	prevWait := waitCollectIdle
	prevApply := applySearchReseal
	t.Cleanup(func() {
		waitCollectIdle = prevWait
		applySearchReseal = prevApply
		collectLifecycle.endSource("empty-seal")
		resetSearchReseal()
	})
	progress.Ensure("empty-seal", "EmptySeal")
	progress.Update("empty-seal", func(p *model.CollectProgress) {
		p.Status = progress.StatusSealing
	})
	if err := collectLifecycle.beginSource("empty-seal"); err != nil {
		t.Fatal(err)
	}

	release := make(chan struct{})
	waitCollectIdle = func() { <-release }
	applySearchReseal = func([]int64) bool { return true }

	searchReseal.addSealing("empty-seal")
	time.Sleep(30 * time.Millisecond)
	if snap, _ := progress.Snapshot("empty-seal"); snap.Status != progress.StatusSealing {
		t.Fatalf("抓页未结束时应保持 sealing, got %q", snap.Status)
	}

	collectLifecycle.endSource("empty-seal")
	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if snap, _ := progress.Snapshot("empty-seal"); snap.Status == progress.StatusDone {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	snap, _ := progress.Snapshot("empty-seal")
	t.Fatalf("空封口周期也应收成 done, got %q", snap.Status)
}

func TestBeginSource_RejectsWhileSearchIndexBuilding(t *testing.T) {
	filmsnapshot.NotifySearchIndexProgress(1, 0, "indexing")
	t.Cleanup(func() {
		filmsnapshot.NotifySearchIndexProgress(0, 0, "idle")
	})
	err := collectLifecycle.beginSource("during-index")
	if err != errSearchIndexUpdating {
		t.Fatalf("err = %v", err)
	}
}
