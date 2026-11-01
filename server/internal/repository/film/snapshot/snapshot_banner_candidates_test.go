package snapshot

import (
	"testing"

	"server/internal/model"
)

func bannerCandidateFilm(mid, pid, hits int64, score float64, updateStamp int64, name string) model.FilmIndex {
	return model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: mid, FirstSourceId: "source_master"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: pid, Cid: pid * 10, CName: "动作片"},
		FilmIndexContent: model.FilmIndexContent{
			Name: name, Hits: hits, Score: score, UpdateStamp: updateStamp,
		},
	}
}

func bannerCandidateMids(snaps []model.FilmListSnapshot) []int64 {
	mids := make([]int64, 0, len(snaps))
	for _, snap := range snaps {
		mids = append(mids, snap.Mid)
	}
	return mids
}

func TestGetSnapshotBannerCandidates_TopNExcludeAndScoreFill(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	rows := []model.FilmIndex{
		bannerCandidateFilm(1, 1, 10, 9.0, 100, "低热高分"),
		bannerCandidateFilm(2, 1, 50, 5.0, 300, "最热低分"),
		bannerCandidateFilm(3, 1, 40, 8.0, 200, "次热高分"),
		bannerCandidateFilm(4, 1, 30, 7.0, 400, "中热达标"),
		bannerCandidateFilm(5, 1, 20, 4.0, 500, "最新低分"),
		bannerCandidateFilm(6, 2, 100, 9.5, 900, "他类更热"),
	}
	if err := gdb.Create(&rows).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	assertMids := func(strategy string, limit int, exclude []int64, want ...int64) {
		t.Helper()
		got := bannerCandidateMids(GetSnapshotBannerCandidates("live", strategy, "", []int64{1}, limit, exclude))
		if len(got) != len(want) {
			t.Fatalf("%s limit=%d exclude=%v got %v want %v", strategy, limit, exclude, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s limit=%d exclude=%v got %v want %v", strategy, limit, exclude, got, want)
			}
		}
	}

	assertMids("hot_random", 2, nil, 2, 3)
	assertMids("hot_random", 2, []int64{2, 3}, 4, 5)
	assertMids("score_random", 3, nil, 1, 3, 4)
	assertMids("score_random", 4, nil, 1, 3, 4, 2)
	assertMids("latest_random", 2, nil, 5, 4)
	if got := GetSnapshotBannerCandidates("live", "hot_random", "", []int64{1}, 0, nil); len(got) != 0 {
		t.Fatalf("limit 0 got %v", bannerCandidateMids(got))
	}
}
