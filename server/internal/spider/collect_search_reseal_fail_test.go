package spider

import (
	"testing"

	"server/internal/model"
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
