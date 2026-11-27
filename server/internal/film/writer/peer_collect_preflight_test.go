package writer

import (
	"context"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
)

func TestSaveCollectedPeerDetails_DeletesMultipleVanishedPlaylistsViaTupleIn(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	src := &model.FilmSource{Id: "src-vanish", Name: "删线路站", State: true}
	initial := testPeerDetail(9901, "多线路剧集", "第1集")
	initial.PlayFrom = []string{"lineA", "lineB", "lineC"}
	initial.PlayList = [][]model.MovieUrlInfo{
		{{Episode: "01", Link: "https://ex/a1.m3u8"}},
		{{Episode: "01", Link: "https://ex/b1.m3u8"}},
		{{Episode: "01", Link: "https://ex/c1.m3u8"}},
	}
	if _, err := SaveCollectedPeerDetails(context.Background(), src, 1, []model.MovieDetail{initial}); err != nil {
		t.Fatalf("initial write: %v", err)
	}

	// 只保留第 1 组线路，淘汰第 2、3 组（len(vanished)=2，触发 tuple IN 批量删除路径）。
	shrunk := initial
	shrunk.UpdateTime = "2026-11-28 00:05:00"
	shrunk.PlayFrom = []string{"lineA"}
	shrunk.PlayList = [][]model.MovieUrlInfo{
		{{Episode: "01", Link: "https://ex/a1.m3u8"}},
	}
	if _, err := SaveCollectedPeerDetails(context.Background(), src, 1, []model.MovieDetail{shrunk}); err != nil {
		t.Fatalf("shrink playlists: %v", err)
	}

	var rows []model.FilmSourcePlaylist
	if err := gdb.Where("source_id = ?", src.Id).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].GroupName != "lineA" {
		t.Fatalf("expected only lineA left after tuple-IN delete, got %+v", rows)
	}
}

func TestClaimPageVodTimesTx_DoesNotRegressStoredUnix(t *testing.T) {
	gdb := openPeerPageDB(t)
	srcID := "src-monotonic"
	if err := gdb.Create(&model.MovieSourceMapping{
		SourceId:      srcID,
		SourceMid:     7001,
		GlobalMid:     1,
		SourceVodUnix: 2000000000,
	}).Error; err != nil {
		t.Fatal(err)
	}

	// 传入滞后于库内 2000000000 的旧快照 map(100)，验证 SQL CASE WHEN 不会将库内值倒写为 2026 年时间戳。
	staleMap := map[int64]int64{7001: 100}
	won, err := claimPageVodTimesTx(gdb, srcID, []vodTimeClaim{{
		sourceMid:  7001,
		updateTime: "2026-01-01 00:00:00",
	}}, staleMap)
	if err != nil || len(won) != 1 || !won[0] {
		t.Fatalf("claim failed: won=%v err=%v", won, err)
	}

	var got model.MovieSourceMapping
	if err := gdb.Where("source_id = ? AND source_mid = ?", srcID, 7001).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.SourceVodUnix != 2000000000 {
		t.Fatalf("source_vod_unix regressed from 2000000000 to %d", got.SourceVodUnix)
	}
}

func TestLockPeerCollectPage_DoesNotBlockWithinSameSourceDistinctVods(t *testing.T) {
	detailA := testPeerDetail(10001, "片A", "第1集")
	detailB := testPeerDetail(10002, "片B", "第1集")
	for sourceVodLockIndex("same-src", detailA.Id) == sourceVodLockIndex("same-src", detailB.Id) {
		detailB.Id++
	}
	src := &model.FilmSource{Id: "same-src", Name: "同站并发"}

	unlockA := lockPeerCollectPage(src, []model.MovieDetail{detailA})
	defer unlockA()

	lockedB := make(chan struct{})
	go func() {
		unlockB := lockPeerCollectPage(src, []model.MovieDetail{detailB})
		unlockB()
		close(lockedB)
	}()

	select {
	case <-lockedB:
	case <-time.After(time.Second):
		t.Fatal("同站不同 source_mid 的页级写锁不应互相阻塞")
	}
}

func TestSaveCollectedPeerDetails_PreflightSkipsUnchangedAndKeepsSameTimeDiff(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	src := &model.FilmSource{Id: "src-preflight", Name: "预检站", State: true}
	first := testPeerDetail(8801, "预检电影", "第1集")
	first.UpdateTime = "2026-11-27 10:00:00"
	mids, err := SaveCollectedPeerDetails(context.Background(), src, 1, []model.MovieDetail{first})
	if err != nil || len(mids.Affected) != 1 {
		t.Fatalf("initial write failed: mids=%+v err=%v", mids, err)
	}

	// 完全一致的重采应直接走预检跳过，不产生新的变更 mid。
	mids2, err := SaveCollectedPeerDetails(context.Background(), src, 1, []model.MovieDetail{first})
	if err != nil || len(mids2.Affected) != 0 {
		t.Fatalf("unchanged page should be skipped by preflight: mids=%+v err=%v", mids2, err)
	}

	// vod_time 未变但片单发生变化时，预检不能误跳过，仍应入库更新。
	updated := testPeerDetail(8801, "预检电影", "第2集")
	updated.UpdateTime = first.UpdateTime
	mids3, err := SaveCollectedPeerDetails(context.Background(), src, 1, []model.MovieDetail{updated})
	if err != nil || len(mids3.Affected) != 1 || len(mids3.Notify) != 1 {
		t.Fatalf("same-time playlist diff must not be skipped: mids=%+v err=%v", mids3, err)
	}
}
