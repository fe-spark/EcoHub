package writer

import (
	"context"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/film/shared"
)

func TestOlderSourcePageDoesNotOverwriteNewerPlaylist(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	source := &model.FilmSource{Id: "src-sony", Name: "索尼", State: true}
	newer := testPeerDetailWithLines(158664, "走马上任", []string{"snm3u8"}, []string{"第01集", "第02集", "第03集", "第04集", "第05集"})
	newer.UpdateTime = "2026-11-02 13:45:05"
	newer.Picture = "https://new/cover.jpg"
	first, err := SaveCollectedPeerDetails(context.Background(), source, 1, []model.MovieDetail{newer})
	if err != nil {
		t.Fatalf("newer page: %v", err)
	}
	if len(first.Notify) != 1 {
		t.Fatalf("newer notify=%v", first.Notify)
	}

	older := testPeerDetailWithLines(158664, "走马上任旧档", []string{"snm3u8"}, []string{"第01集", "第02集", "第03集", "第04集"})
	older.UpdateTime = "2026-11-02 12:10:06"
	older.Picture = "https://old/cover.jpg"
	second, err := SaveCollectedPeerDetails(context.Background(), source, 2, []model.MovieDetail{older})
	if err != nil {
		t.Fatalf("older page: %v", err)
	}
	if len(second.Notify) != 0 || len(second.Affected) != 0 {
		t.Fatalf("older page is not an update: notify=%v affected=%v", second.Notify, second.Affected)
	}
	var film model.FilmIndex
	if err := gdb.Where("mid = ?", first.Notify[0]).First(&film).Error; err != nil {
		t.Fatal(err)
	}
	if film.Name != "走马上任" || film.Picture != "https://new/cover.jpg" {
		t.Fatalf("older page overwrote film: name=%s picture=%s", film.Name, film.Picture)
	}
	assertMatchKeys(t, gdb, film.Mid, shared.BuildMovieMatchKeysWithCategory(0, "走马上任", film.Pid))
	var lines []model.FilmSourcePlaylist
	if err := gdb.Where("source_id = ? AND line_kind = ?", source.Id, "play").Find(&lines).Error; err != nil {
		t.Fatal(err)
	}
	if len(lines) != 5 {
		t.Fatalf("older page overwrote playlist: groups=%d", len(lines))
	}

	again, err := SaveCollectedPeerDetails(context.Background(), source, 1, []model.MovieDetail{newer})
	if err != nil {
		t.Fatalf("replay newer: %v", err)
	}
	if len(again.Notify) != 0 || len(again.Affected) != 0 {
		t.Fatalf("identical replay is not an update: notify=%v affected=%v", again.Notify, again.Affected)
	}
}

func TestSaveSinglePeerDetail_StaleSnapshotRejected(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	source := &model.FilmSource{Id: "src-single", Name: "单站", State: true}
	newer := testPeerDetailWithLines(1001, "新片", []string{"线路"}, []string{"第1集", "第2集"})
	newer.UpdateTime = "2026-11-02 14:00:00"
	mid, isNew, changed, _, err := saveSinglePeerDetailTx(source, newer)
	if err != nil || mid <= 0 || !isNew {
		t.Fatalf("save new film: mid=%d isNew=%v changed=%v err=%v", mid, isNew, changed, err)
	}

	older := testPeerDetailWithLines(1001, "新片", []string{"线路"}, []string{"第1集"})
	older.UpdateTime = "2026-11-02 13:00:00"
	staleMid, staleIsNew, staleChanged, _, err := saveSinglePeerDetailTx(source, older)
	if err != nil {
		t.Fatalf("save older film: %v", err)
	}
	if staleMid != 0 || staleIsNew || staleChanged {
		t.Fatalf("older snapshot is not an update: mid=%d isNew=%v changed=%v", staleMid, staleIsNew, staleChanged)
	}
}

func TestClaimSourceVodTimeTx_OlderTimeIsNotAnUpdate(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	mapping := model.MovieSourceMapping{
		SourceId:      "src-race",
		SourceMid:     2001,
		GlobalMid:     888,
		SourceVodUnix: 1000,
	}
	if err := gdb.Create(&mapping).Error; err != nil {
		t.Fatal(err)
	}

	wonNew, err := claimSourceVodTimeTx(gdb, "src-race", 2001, "2026-11-02 13:00:00")
	if err != nil || !wonNew {
		t.Fatalf("newer update should win: won=%v err=%v", wonNew, err)
	}
	wonOld, err := claimSourceVodTimeTx(gdb, "src-race", 2001, "2026-11-02 12:00:00")
	if err != nil {
		t.Fatalf("older update check error: %v", err)
	}
	if wonOld {
		t.Fatal("older vod_time is not an update")
	}
	wonSame, err := claimSourceVodTimeTx(gdb, "src-race", 2001, "2026-11-02 13:00:00")
	if err != nil || !wonSame {
		t.Fatalf("same timestamp can be compared for content change: won=%v err=%v", wonSame, err)
	}
}
