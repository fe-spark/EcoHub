package spider

import (
	"testing"

	"server/internal/model"
)

func TestPrioritizeCollectSourcesWeightFirst(t *testing.T) {
	sources := []model.FilmSource{
		{Id: "s1", Name: "A", Weight: 10},
		{Id: "m1", Name: "M", Weight: 50},
		{Id: "s2", Name: "B", Weight: 10},
	}
	out := prioritizeCollectSources(sources)
	if len(out) != 3 || out[0].Id != "m1" {
		t.Fatalf("highest weight should be first, got %+v", out)
	}
	if out[1].Id != "s1" || out[2].Id != "s2" {
		t.Fatalf("same weight order should be stable/id-sorted, got %+v", out)
	}
}
