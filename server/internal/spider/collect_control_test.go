package spider

import (
	"testing"

	"server/internal/model"
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
