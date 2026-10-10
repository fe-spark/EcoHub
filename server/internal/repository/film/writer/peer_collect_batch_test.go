package writer

import (
	"context"
	"errors"
	"strings"
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

func TestFitLastEpisodeKeepsShortLabels(t *testing.T) {
	if got := fitLastEpisode("第9集", 9); got != "第9集" {
		t.Fatalf("episode=%q", got)
	}
	if got := fitLastEpisode("只剩一集", 1); got != "只剩一集" {
		t.Fatalf("custom=%q", got)
	}
	glued := "第04集https://v2.ppqrrs.com/wjv2/202607/18/abc/video/index.m3u8"
	if got := fitLastEpisode(glued, 4); got != "第4集" {
		t.Fatalf("glued=%q", got)
	}
	if got := fitLastEpisode(strings.Repeat("集", 65), 3); got != "第3集" {
		t.Fatalf("long=%q", got)
	}
}

func TestSaveCollectedPeerDetails_SkipsOversizedFilm(t *testing.T) {
	gdb := openPeerPageDB(t)
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	// 单片回放会在事务里再查采集站列表，需要第二条连接。
	sqlDB.SetMaxOpenConns(2)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	origWrite := writePeerPage
	writePeerPage = func(tx *gorm.DB, source *model.FilmSource, details []model.MovieDetail, sources []model.FilmSource) ([]int64, []int64, error) {
		return nil, nil, errors.New("Error 1406 (22001): Data too long for column 'release_date' at row 1")
	}
	origSave := savePeerFilm
	savePeerFilm = func(source *model.FilmSource, detail model.MovieDetail) (int64, bool, bool, error) {
		if detail.Id == 12 {
			return 0, false, false, errors.New("Error 1406 (22001): Data too long for column 'last_episode' at row 5")
		}
		return origSave(source, detail)
	}
	t.Cleanup(func() {
		writePeerPage = origWrite
		savePeerFilm = origSave
	})

	source := &model.FilmSource{Id: "src-skip", Name: "甲站", State: true}
	got, err := SaveCollectedPeerDetails(context.Background(), source, 7, []model.MovieDetail{
		testPeerDetail(11, "电影甲", "第1集"),
		testPeerDetail(12, "电影乙", "第04集https://cdn.example/a.m3u8"),
	})
	if err != nil {
		t.Fatalf("page should succeed after skipping the bad film: %v", err)
	}
	if len(got.Affected) != 1 {
		t.Fatalf("affected=%v", got.Affected)
	}
	if countRows(t, gdb, &model.FilmIndex{}) != 1 {
		t.Fatal("oversized film was stored")
	}
}

func TestSaveCollectedPeerDetails_TransientFilmErrorRetriesPage(t *testing.T) {
	gdb := openPeerPageDB(t)
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(2)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	origWrite := writePeerPage
	writePeerPage = func(tx *gorm.DB, source *model.FilmSource, details []model.MovieDetail, sources []model.FilmSource) ([]int64, []int64, error) {
		return nil, nil, errors.New("Error 1406 (22001): Data too long for column 'release_date' at row 1")
	}
	origSave := savePeerFilm
	savePeerFilm = func(source *model.FilmSource, detail model.MovieDetail) (int64, bool, bool, error) {
		if detail.Id == 12 {
			return 0, false, false, errors.New("connection reset by peer")
		}
		return origSave(source, detail)
	}
	t.Cleanup(func() {
		writePeerPage = origWrite
		savePeerFilm = origSave
	})

	source := &model.FilmSource{Id: "src-retry", Name: "甲站", State: true}
	_, err = SaveCollectedPeerDetails(context.Background(), source, 8, []model.MovieDetail{
		testPeerDetail(11, "电影甲", "第1集"),
		testPeerDetail(12, "电影乙", "第2集"),
	})
	if err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("transient error=%v", err)
	}
}

func openPeerPageDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
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
