package collect

import (
	"server/internal/model"
	"testing"
	"time"
)

func TestPrioritizeCollectSourcesSortFirst(t *testing.T) {
	sources := []model.FilmSource{
		{Id: "s1", Name: "A", Sort: 1},
		{Id: "m1", Name: "M", Sort: 0},
		{Id: "s2", Name: "B", Sort: 1},
	}
	out := prioritizeCollectSources(sources)
	if len(out) != 3 || out[0].Id != "m1" {
		t.Fatalf("lowest sort should be first, got %+v", out)
	}
	if out[1].Id != "s1" || out[2].Id != "s2" {
		t.Fatalf("same sort order should be stable/id-sorted, got %+v", out)
	}
}

// 后台采集批次 panic 被兜住，且仍从 backgroundCollects 里摘掉。
func TestGoBackgroundCollect_RecoversPanic(t *testing.T) {
	goBackgroundCollect(func() { panic("boom") })
	if !WaitBackgroundCollects(2 * time.Second) {
		t.Fatal("panicking background collect must still be marked done")
	}
}
