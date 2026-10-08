package writer

import (
	"context"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMatchKeyLockStripes_CoverConcurrentPages(t *testing.T) {
	if matchKeyLockStripes < 65536 {
		t.Fatalf("match key stripes = %d", matchKeyLockStripes)
	}
	if len(filmMatchKeyLocks) != matchKeyLockStripes {
		t.Fatalf("lock array = %d", len(filmMatchKeyLocks))
	}
}

func TestSaveCollectedPeerDetails_PageCommit(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	sourceA := &model.FilmSource{Id: "src-a", Name: "甲站", State: true}
	first, err := SaveCollectedPeerDetails(context.Background(), sourceA, 1, []model.MovieDetail{
		testPeerDetail(11, "电影甲", "第1集"),
		testPeerDetail(12, "电影乙", "第2集"),
		{Id: 0, Name: "丢掉"},
		{Id: 13, Name: "   "},
	})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Affected) != 2 || len(first.Notify) != 2 {
		t.Fatalf("affected=%v notify=%v", first.Affected, first.Notify)
	}
	if countRows(t, gdb, &model.FilmIndex{}) != 2 {
		t.Fatal("expected 2 films")
	}
	if countRows(t, gdb, &model.MovieSourceMapping{}) != 2 {
		t.Fatal("expected 2 mappings")
	}
	if countRows(t, gdb, &model.FilmSourcePlaylist{}) != 2 {
		t.Fatal("expected 2 playlists")
	}
	var line model.FilmSourcePlaylist
	if err := gdb.Where("group_index = ?", 0).First(&line).Error; err != nil {
		t.Fatal(err)
	}
	if line.GroupIndex != 0 || line.Content == "" {
		t.Fatalf("group 0 not stored: %+v", line)
	}

	again, err := SaveCollectedPeerDetails(context.Background(), sourceA, 1, []model.MovieDetail{
		testPeerDetail(11, "电影甲", "第1集"),
		testPeerDetail(12, "电影乙", "第2集"),
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(again.Notify) != 0 {
		t.Fatalf("identical replay should not notify: %v", again.Notify)
	}
	if countRows(t, gdb, &model.FilmIndex{}) != 2 || countRows(t, gdb, &model.FilmSourcePlaylist{}) != 2 {
		t.Fatal("replay duplicated rows")
	}

	sourceB := &model.FilmSource{Id: "src-b", Name: "乙站", State: true}
	second, err := SaveCollectedPeerDetails(context.Background(), sourceB, 3, []model.MovieDetail{
		testPeerDetail(21, "电影甲", "第1集"),
	})
	if err != nil {
		t.Fatalf("second source: %v", err)
	}
	if len(second.Affected) != 1 {
		t.Fatalf("second affected=%v", second.Affected)
	}
	if countRows(t, gdb, &model.FilmIndex{}) != 2 {
		t.Fatal("matched film was inserted again")
	}
	if countRows(t, gdb, &model.MovieSourceMapping{}) != 3 {
		t.Fatal("second source mapping missing")
	}
	var playCount int64
	if err := gdb.Model(&model.FilmSourcePlaylist{}).Where("mid = ?", second.Affected[0]).Count(&playCount).Error; err != nil {
		t.Fatal(err)
	}
	if playCount != 2 {
		t.Fatalf("shared film playlists=%d", playCount)
	}

	merged, err := SaveCollectedPeerDetails(context.Background(), sourceA, 4, []model.MovieDetail{
		testPeerDetail(31, "同名片", "第1集"),
		testPeerDetail(32, "同名片", "第9集"),
	})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(merged.Affected) != 1 {
		t.Fatalf("same title should be one film, affected=%v", merged.Affected)
	}
	if countRows(t, gdb, &model.FilmIndex{}) != 3 {
		t.Fatal("same-page title was not merged")
	}
	var mergedMaps int64
	if err := gdb.Model(&model.MovieSourceMapping{}).Where("global_mid = ?", merged.Affected[0]).Count(&mergedMaps).Error; err != nil {
		t.Fatal(err)
	}
	if mergedMaps != 2 {
		t.Fatalf("merged mappings=%d", mergedMaps)
	}
	var mergedLines model.FilmSourcePlaylist
	if err := gdb.Where("source_id = ? AND mid = ?", sourceA.Id, merged.Affected[0]).First(&mergedLines).Error; err != nil {
		t.Fatal(err)
	}
	if mergedLines.LastEpisode != "第9集" {
		t.Fatalf("last detail should win, episode=%q", mergedLines.LastEpisode)
	}

	updated, err := SaveCollectedPeerDetails(context.Background(), sourceA, 5, []model.MovieDetail{
		testPeerDetailWithLines(11, "电影甲", []string{"正片", "备用"}, []string{"新1", "新2"}),
	})
	if err != nil {
		t.Fatalf("update lines: %v", err)
	}
	if len(updated.Notify) != 1 {
		t.Fatalf("play change should notify: %v", updated.Notify)
	}
	shrunk, err := SaveCollectedPeerDetails(context.Background(), sourceA, 6, []model.MovieDetail{
		testPeerDetail(11, "电影甲", "只剩一集"),
	})
	if err != nil {
		t.Fatalf("shrink lines: %v", err)
	}
	if len(shrunk.Notify) != 1 {
		t.Fatalf("shrink should notify: %v", shrunk.Notify)
	}
	var left int64
	if err := gdb.Model(&model.FilmSourcePlaylist{}).
		Where("source_id = ? AND mid = ? AND line_kind = ?", sourceA.Id, updated.Notify[0], "play").
		Count(&left).Error; err != nil {
		t.Fatal(err)
	}
	if left != 1 {
		t.Fatalf("vanished line still present, count=%d", left)
	}
	var stored model.FilmSourcePlaylist
	if err := gdb.Where("source_id = ? AND mid = ? AND group_index = ?", sourceA.Id, updated.Notify[0], 0).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.LastEpisode != "只剩一集" {
		t.Fatalf("last episode=%q", stored.LastEpisode)
	}
}

func openPeerPageDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:peer_page_batch?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := gdb.AutoMigrate(
		&model.FilmSource{},
		&model.FilmIndex{},
		&model.MovieMatchKey{},
		&model.MovieSourceMapping{},
		&model.FilmSourcePlaylist{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return gdb
}

func testPeerDetail(id int64, name, episode string) model.MovieDetail {
	return testPeerDetailWithLines(id, name, []string{"线路"}, []string{episode})
}

func testPeerDetailWithLines(id int64, name string, from []string, episodes []string) model.MovieDetail {
	play := make([][]model.MovieUrlInfo, 0, len(episodes))
	for _, episode := range episodes {
		play = append(play, []model.MovieUrlInfo{{Episode: episode, Link: "http://play/" + episode}})
	}
	return model.MovieDetail{
		Id:              id,
		Name:            name,
		MovieDescriptor: model.MovieDescriptor{UpdateTime: "2026-10-08 12:00:00"},
		PlayFrom:        from,
		PlayList:        play,
	}
}

func countRows(t *testing.T, gdb *gorm.DB, modelValue any) int64 {
	t.Helper()
	var n int64
	if err := gdb.Model(modelValue).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}
