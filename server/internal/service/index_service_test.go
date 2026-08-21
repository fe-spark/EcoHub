package service

import (
	"testing"

	"server/internal/model"
)

func TestPickRandomMovieInfosExcludesCurrentBatch(t *testing.T) {
	src := make([]model.MovieBasicInfo, 0, 20)
	for i := int64(1); i <= 20; i++ {
		src = append(src, model.MovieBasicInfo{Id: i, Name: "m"})
	}
	exclude := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	got := pickRandomMovieInfos(src, 12, exclude)
	if len(got) != 8 {
		t.Fatalf("want remaining 8, got %d", len(got))
	}
	skip := map[int64]struct{}{}
	for _, id := range exclude {
		skip[id] = struct{}{}
	}
	for _, item := range got {
		if _, hit := skip[item.Id]; hit {
			t.Fatalf("excluded id %d appeared", item.Id)
		}
	}
}

func TestPickRandomMovieInfosWrapsWhenAllExcluded(t *testing.T) {
	src := []model.MovieBasicInfo{{Id: 1}, {Id: 2}, {Id: 3}}
	got := pickRandomMovieInfos(src, 12, []int64{1, 2, 3})
	if len(got) != 3 {
		t.Fatalf("want wrap to full pool 3, got %d", len(got))
	}
}

func TestSelectDailyUpdatesAllWhenLimitNotPositive(t *testing.T) {
	src := []model.MovieBasicInfo{{Id: 1}, {Id: 2}, {Id: 3}}
	got := selectDailyUpdates(src, 0, []int64{1})
	if len(got) != 3 {
		t.Fatalf("want all 3, got %d", len(got))
	}
	if got[0].Id != 1 || got[2].Id != 3 {
		t.Fatalf("want original order, got %+v", got)
	}
}

func TestSelectDailyUpdatesEmptyPool(t *testing.T) {
	got := selectDailyUpdates(nil, 0, nil)
	if got == nil || len(got) != 0 {
		t.Fatalf("want empty slice, got %#v", got)
	}
}
