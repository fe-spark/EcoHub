package writer

import (
	"context"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/film/shared"
)

func TestSameSourceTitleDoesNotClaimExistingFilm(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	source := &model.FilmSource{Id: "src-batch", Name: "乙", State: true}
	first, err := SaveCollectedPeerDetails(context.Background(), source, 1, []model.MovieDetail{
		testPeerDetail(81, "仙逆", "第1集"),
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := SaveCollectedPeerDetails(context.Background(), source, 2, []model.MovieDetail{
		testPeerDetail(82, "仙逆", "第9集"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Affected) != 1 || len(second.Affected) != 1 || first.Affected[0] == second.Affected[0] {
		t.Fatalf("same title vods must not share a film: %v %v", first.Affected, second.Affected)
	}
	replay, err := SaveCollectedPeerDetails(context.Background(), source, 3, []model.MovieDetail{
		testPeerDetail(81, "仙逆", "第1集"),
		testPeerDetail(82, "仙逆", "第9集"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(replay.Notify) != 0 {
		t.Fatalf("unchanged separate vods should not notify: %v", replay.Notify)
	}
}

func TestSharedMappingSplitsOnNextCollect(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	source := &model.FilmSource{Id: "src-batch", Name: "乙", State: true}
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 7, FirstSourceId: source.Id},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1, Cid: 1},
		FilmIndexContent:  model.FilmIndexContent{Name: "现在就出发第4季"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	for _, vod := range []int64{203002, 204295} {
		if err := gdb.Create(&model.MovieSourceMapping{SourceId: source.Id, SourceMid: vod, GlobalMid: 7}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := gdb.Create(&model.FilmSourcePlaylist{
		Mid: 7, SourceId: source.Id, LineKind: "play", GroupIndex: 0, GroupName: "lzm3u8",
		EpisodeCount: 1, LastEpisode: "旧集", Content: `[{"episode":"旧集","link":"http://old"}]`, ContentHash: "old",
	}).Error; err != nil {
		t.Fatal(err)
	}

	written, err := SaveCollectedPeerDetails(context.Background(), source, 1, []model.MovieDetail{
		testPeerDetail(203002, "现在就出发第4季", "旧集"),
		testPeerDetail(204295, "现在就出发第4季", "另一套"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var maps []model.MovieSourceMapping
	if err = gdb.Where("source_id = ?", source.Id).Order("source_mid").Find(&maps).Error; err != nil {
		t.Fatal(err)
	}
	if len(maps) != 2 || maps[0].GlobalMid == maps[1].GlobalMid {
		t.Fatalf("shared mapping was not split: %+v written=%v", maps, written.Affected)
	}
	if maps[0].SourceMid != 203002 || maps[0].GlobalMid != 7 {
		t.Fatalf("smallest vod should keep the old film: %+v", maps[0])
	}
	replay, err := SaveCollectedPeerDetails(context.Background(), source, 2, []model.MovieDetail{
		testPeerDetail(203002, "现在就出发第4季", "旧集"),
		testPeerDetail(204295, "现在就出发第4季", "另一套"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(replay.Notify) != 0 {
		t.Fatalf("split vods should not notify again: %v", replay.Notify)
	}
}

func TestOtherSourceTitleIsNotClaimed(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	titleKeys := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 0)
	if len(titleKeys) != 1 {
		t.Fatalf("title=%v", titleKeys)
	}
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, FirstSourceId: "other"},
		FilmIndexContent:  model.FilmIndexContent{Name: "仙逆"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.MovieMatchKey{Mid: 1, MatchKey: titleKeys[0]}).Error; err != nil {
		t.Fatal(err)
	}
	written, err := SaveCollectedPeerDetails(context.Background(), &model.FilmSource{Id: "src-batch", Name: "乙", State: true}, 1, []model.MovieDetail{
		testPeerDetail(82, "仙逆", "第1集"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(written.Affected) != 1 || written.Affected[0] == 1 {
		t.Fatalf("claimed another source film: %v", written.Affected)
	}
}
