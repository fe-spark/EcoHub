package progress

import (
	"testing"

	"server/internal/model"
)

func TestSetPageTotalSetsProbedOnlyWhenStarting(t *testing.T) {
	const id = "page-total-probed"
	store.Delete(id)
	t.Cleanup(func() { store.Delete(id) })

	SetPageTotal(id, 3)
	if PageTotal(id) != -1 {
		t.Fatal("没有进度时不能记成已探测")
	}

	MarkSourcesCollectStarting([]model.FilmSource{{Id: id, Name: "T"}}, "q1")
	SetPageTotal(id, 4)
	if got := PageTotal(id); got != 4 {
		t.Fatalf("已探测总页 = %d，期望 4", got)
	}

	state := ensure(id, "T")
	state.mu.Lock()
	state.data.Status = StatusRunning
	state.probed = false
	state.data.Total = 9
	state.mu.Unlock()
	SetPageTotal(id, 1)
	if PageTotal(id) != -1 {
		t.Fatal("采集中的迟到探测不能标成已探测")
	}
	snap, ok := Snapshot(id)
	if !ok || snap.Total != 9 || snap.Status != StatusRunning {
		t.Fatalf("迟到探测改了进度: ok=%v %+v", ok, snap)
	}

	MarkSourcesCollectStarting([]model.FilmSource{{Id: id, Name: "T"}}, "q2")
	if PageTotal(id) != -1 {
		t.Fatal("新一轮采集必须清掉已探测")
	}
	SetPageTotal(id, 0)
	if PageTotal(id) != 0 {
		t.Fatalf("0 页应记成已探测，得到 %d", PageTotal(id))
	}
	snap, ok = Snapshot(id)
	if !ok || snap.Status != StatusDone {
		t.Fatalf("0 页应直接完成: ok=%v status=%q", ok, snap.Status)
	}
}
