package spider

import (
	"testing"

	"server/internal/model"
)

func TestPrioritizeCollectSourcesMasterFirst(t *testing.T) {
	sources := []model.FilmSource{
		{Id: "s1", Name: "A", Grade: model.SlaveCollect},
		{Id: "m1", Name: "M", Grade: model.MasterCollect},
		{Id: "s2", Name: "B", Grade: model.SlaveCollect},
	}
	out := prioritizeCollectSources(sources)
	if len(out) != 3 || out[0].Id != "m1" {
		t.Fatalf("master should be first, got %+v", out)
	}
	if out[1].Id != "s1" || out[2].Id != "s2" {
		t.Fatalf("slave order should be stable, got %+v", out)
	}
}
