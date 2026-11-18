package spider

import (
	"testing"

	"server/internal/model"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider/progress"
)

func TestSealFrozenRound_RestoresMidsWhenSealFails(t *testing.T) {
	resetSearchReseal()
	prevSeal := sealSearchIndex
	prevWait := sealRetryWait
	t.Cleanup(func() {
		sealSearchIndex = prevSeal
		sealRetryWait = prevWait
		resetSearchReseal()
	})
	sealRetryWait = 0
	progress.Ensure("fail-seal", "失败站")
	progress.Update("fail-seal", func(p *model.CollectProgress) {
		p.Status = progress.StatusSealing
	})

	attempts := 0
	sealSearchIndex = func(mids []int64) bool {
		attempts++
		if len(mids) != 2 {
			t.Errorf("mids=%d", len(mids))
		}
		return attempts > 1
	}

	searchReseal.mu.Lock()
	searchReseal.pending[11] = struct{}{}
	searchReseal.pending[12] = struct{}{}
	searchReseal.sealing["fail-seal"] = struct{}{}
	searchReseal.running = true
	searchReseal.mu.Unlock()

	if searchReseal.sealFrozenRound() {
		t.Fatal("封口失败应留下这一轮")
	}
	searchReseal.mu.Lock()
	pending := len(searchReseal.pending)
	_, sealing := searchReseal.sealing["fail-seal"]
	searchReseal.mu.Unlock()
	if pending != 2 || !sealing {
		t.Fatalf("pending=%d sealing=%v", pending, sealing)
	}
	if snap, ok := progress.Snapshot("fail-seal"); !ok || snap.Status != progress.StatusSealing {
		t.Fatalf("status=%s ok=%v", snap.Status, ok)
	}

	if searchReseal.sealFrozenRound() {
		t.Fatal("封口成功后应回到循环")
	}
	searchReseal.mu.Lock()
	pending = len(searchReseal.pending)
	_, sealing = searchReseal.sealing["fail-seal"]
	searchReseal.mu.Unlock()
	if pending != 0 || sealing {
		t.Fatalf("pending=%d sealing=%v", pending, sealing)
	}
	if snap, ok := progress.Snapshot("fail-seal"); !ok || snap.Status != progress.StatusDone {
		t.Fatalf("status=%s ok=%v", snap.Status, ok)
	}
}

func TestSealFrozenRound_RestoresMidsEvenIfProgressChangesWithoutRebuild(t *testing.T) {
	resetSearchReseal()
	prevSeal := sealSearchIndex
	prevWait := sealRetryWait
	t.Cleanup(func() {
		sealSearchIndex = prevSeal
		sealRetryWait = prevWait
		resetSearchReseal()
	})
	sealRetryWait = 0

	sealSearchIndex = func(mids []int64) bool {
		// 模拟封口失败（未触发重建），期间另一路触发进度变更为 waiting
		filmsnapshot.NotifySearchIndexProgress(0, 0, "waiting")
		return false
	}

	searchReseal.mu.Lock()
	searchReseal.pending[21] = struct{}{}
	searchReseal.pending[22] = struct{}{}
	searchReseal.running = true
	searchReseal.mu.Unlock()

	if searchReseal.sealFrozenRound() {
		t.Fatal("未触发全量重建的封口失败应放回编号重试")
	}
	searchReseal.mu.Lock()
	_, has21 := searchReseal.pending[21]
	_, has22 := searchReseal.pending[22]
	searchReseal.mu.Unlock()
	if !has21 || !has22 {
		t.Fatalf("progress token 变更但不涉及重建时不应丢失 mids, has21=%v has22=%v", has21, has22)
	}
}
