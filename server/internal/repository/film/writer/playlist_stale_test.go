package writer

import (
	"context"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
)

func TestOlderSourcePageDoesNotOverwriteNewerPlaylist(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	source := &model.FilmSource{Id: "src-sony", Name: "索尼", State: true}
	newer := testPeerDetailWithLines(158664, "走马上任", []string{"snm3u8"}, []string{"第01集", "第02集", "第03集", "第04集", "第05集"})
	newer.UpdateTime = "2026-11-02 13:45:05"
	first, err := SaveCollectedPeerDetails(context.Background(), source, 1, []model.MovieDetail{newer})
	if err != nil {
		t.Fatalf("newer page: %v", err)
	}
	if len(first.Notify) != 1 {
		t.Fatalf("newer notify=%v", first.Notify)
	}

	older := testPeerDetailWithLines(158664, "走马上任", []string{"snm3u8"}, []string{"第01集", "第02集", "第03集", "第04集"})
	older.UpdateTime = "2026-11-02 12:10:06"
	second, err := SaveCollectedPeerDetails(context.Background(), source, 2, []model.MovieDetail{older})
	if err != nil {
		t.Fatalf("older page: %v", err)
	}
	if len(second.Notify) != 0 {
		t.Fatalf("older page should not notify: %v", second.Notify)
	}
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
	if len(again.Notify) != 0 {
		t.Fatalf("identical newer replay should not notify: %v", again.Notify)
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
		t.Fatalf("expected stale film to return zero: mid=%d isNew=%v changed=%v", staleMid, staleIsNew, staleChanged)
	}
}

func TestClaimSourceVodTimeTx_ConcurrentRaceWonByNewer(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	// 初始状态：source_vod_unix = 1000
	mapping := model.MovieSourceMapping{
		SourceId:      "src-race",
		SourceMid:     2001,
		GlobalMid:     888,
		SourceVodUnix: 1000,
	}
	if err := gdb.Create(&mapping).Error; err != nil {
		t.Fatal(err)
	}

	// Worker B (新版本: 2026-11-02 13:00:00) 抢先成功写入
	wonNew, err := claimSourceVodTimeTx(gdb, "src-race", 2001, "2026-11-02 13:00:00")
	if err != nil || !wonNew {
		t.Fatalf("newer update should win: won=%v err=%v", wonNew, err)
	}

	// Worker A (旧版本: 2026-11-02 12:00:00) 随后尝试写入，必须被 CAS 拒绝
	wonOld, err := claimSourceVodTimeTx(gdb, "src-race", 2001, "2026-11-02 12:00:00")
	if err != nil {
		t.Fatalf("older update check error: %v", err)
	}
	if wonOld {
		t.Fatalf("older update must be rejected when newer update already committed")
	}

	// 再次验证同时间更新允许放行
	wonSame, err := claimSourceVodTimeTx(gdb, "src-race", 2001, "2026-11-02 13:00:00")
	if err != nil || !wonSame {
		t.Fatalf("same timestamp update should pass without blocking: won=%v err=%v", wonSame, err)
	}
}
