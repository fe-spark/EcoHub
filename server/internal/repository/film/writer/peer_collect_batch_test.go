package writer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/film/shared"
	"server/internal/repository/support"
	"server/internal/utils"

	"github.com/glebarez/sqlite"
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
	if countRows(t, gdb, &model.FilmIndex{}) != 3 {
		t.Fatal("other source should keep its own film")
	}
	if countRows(t, gdb, &model.MovieSourceMapping{}) != 3 {
		t.Fatal("second source mapping missing")
	}
	for _, mid := range first.Affected {
		if second.Affected[0] == mid {
			t.Fatal("second source reused the first source mid")
		}
	}
	var owner model.FilmIndex
	if err := gdb.Where("mid = ?", second.Affected[0]).First(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if owner.FirstSourceId != sourceB.Id {
		t.Fatalf("owner=%s", owner.FirstSourceId)
	}
	var playCount int64
	if err := gdb.Model(&model.FilmSourcePlaylist{}).Where("mid = ?", second.Affected[0]).Count(&playCount).Error; err != nil {
		t.Fatal(err)
	}
	if playCount != 1 {
		t.Fatalf("own film playlists=%d", playCount)
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
	if countRows(t, gdb, &model.FilmIndex{}) != 4 {
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
		&model.Category{},
		&model.CategoryMapping{},
		&model.SourceCategory{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电影", Show: true}).Error
	db.Mdb = gdb
	support.RefreshCategoryCache()
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
		Pid:             1,
		MovieDescriptor: model.MovieDescriptor{UpdateTime: "2026-10-08 12:00:00", CName: "电影"},
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

func TestSaveCollectedPeerDetails_NewFilmKeepsVodTimeAndFollowupUsesLocalTime(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	source := &model.FilmSource{Id: "src-time", Name: "甲站", State: true}
	vodTimeStr := "2020-05-01 10:00:00"
	expectedVodStamp, err := utils.ParseCollectUpdateTime(vodTimeStr)
	if err != nil {
		t.Fatalf("parse vod time: %v", err)
	}

	detail := testPeerDetail(101, "老片首入库", "第1集")
	detail.UpdateTime = vodTimeStr

	res, err := SaveCollectedPeerDetails(context.Background(), source, 1, []model.MovieDetail{detail})
	if err != nil {
		t.Fatalf("first collect: %v", err)
	}
	mid := res.Affected[0]

	var fi model.FilmIndex
	if err := gdb.Where("mid = ?", mid).First(&fi).Error; err != nil {
		t.Fatal(err)
	}
	if fi.UpdateStamp != expectedVodStamp {
		t.Fatalf("new film should keep vod_time stamp %d, got %d", expectedVodStamp, fi.UpdateStamp)
	}

	// 仅下载线路变化：不应刷新 update_stamp
	detailWithDown := detail
	detailWithDown.DownFrom = "down"
	detailWithDown.DownloadList = [][]model.MovieUrlInfo{{{Episode: "种子1", Link: "magnet:?xt=1"}}}
	if _, err := SaveCollectedPeerDetails(context.Background(), source, 1, []model.MovieDetail{detailWithDown}); err != nil {
		t.Fatalf("download update: %v", err)
	}
	if err := gdb.Where("mid = ?", mid).First(&fi).Error; err != nil {
		t.Fatal(err)
	}
	if fi.UpdateStamp != expectedVodStamp {
		t.Fatalf("download-only change should not update stamp: want %d, got %d", expectedVodStamp, fi.UpdateStamp)
	}

	// 续集追更（播放线路变化）：应刷新为当前本地采集时间
	beforeFollowup := time.Now().Unix()
	followup := testPeerDetail(101, "老片首入库", "第2集")
	followup.UpdateTime = vodTimeStr
	if _, err := SaveCollectedPeerDetails(context.Background(), source, 1, []model.MovieDetail{followup}); err != nil {
		t.Fatalf("followup update: %v", err)
	}
	if err := gdb.Where("mid = ?", mid).First(&fi).Error; err != nil {
		t.Fatal(err)
	}
	if fi.UpdateStamp < beforeFollowup {
		t.Fatalf("followup play change should use local collect time >= %d, got %d", beforeFollowup, fi.UpdateStamp)
	}
}

func TestRecollect_RewritesMatchKeysAndKeepsEmptyFields(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	source := &model.FilmSource{Id: "src-key", Name: "甲站", State: true}
	first := testPeerDetail(7, "仙逆", "第1集")
	first.DbId = 36117912
	first.Picture = "https://a/p.jpg"
	first.PictureSlide = "https://a/s.jpg"
	res, err := SaveCollectedPeerDetails(context.Background(), source, 1, []model.MovieDetail{first})
	if err != nil {
		t.Fatalf("first collect: %v", err)
	}
	if len(res.Affected) != 1 {
		t.Fatalf("affected=%v", res.Affected)
	}
	mid := res.Affected[0]
	if err := gdb.Model(&model.FilmIndex{}).Where("mid = ?", mid).Update("pid", int64(3)).Error; err != nil {
		t.Fatal(err)
	}

	second := testPeerDetail(7, "仙逆", "第1集")
	second.Pid = 0
	second.CName = ""
	second.Picture = ""
	second.PictureSlide = ""
	if _, err := SaveCollectedPeerDetails(context.Background(), source, 1, []model.MovieDetail{second}); err != nil {
		t.Fatalf("empty recollect: %v", err)
	}
	var fi model.FilmIndex
	if err := gdb.Where("mid = ?", mid).First(&fi).Error; err != nil {
		t.Fatal(err)
	}
	if fi.DbId != 36117912 || fi.Name != "仙逆" || fi.Pid != 3 || fi.Picture != "https://a/p.jpg" || fi.PictureSlide != "https://a/s.jpg" {
		t.Fatalf("empty recollect changed film: %+v", fi)
	}
	assertMatchKeys(t, gdb, mid, shared.BuildMovieMatchKeysWithCategory(36117912, "仙逆", 3))

	third := testPeerDetail(7, "仙逆年番", "第2集")
	third.Pid = 3
	third.DbId = 36117912
	third.Picture = "https://a/p2.jpg"
	if _, err := SaveCollectedPeerDetails(context.Background(), source, 1, []model.MovieDetail{third}); err != nil {
		t.Fatalf("rename collect: %v", err)
	}
	if err := gdb.Where("mid = ?", mid).First(&fi).Error; err != nil {
		t.Fatal(err)
	}
	if fi.Name != "仙逆年番" || fi.DbId != 36117912 || fi.Picture != "https://a/p2.jpg" || fi.PictureSlide != "https://a/s.jpg" || fi.Pid != 3 {
		t.Fatalf("rename collect film: %+v", fi)
	}
	assertMatchKeys(t, gdb, mid, shared.BuildMovieMatchKeysWithCategory(36117912, "仙逆年番", 3))
	stale := shared.BuildMovieMatchKeysWithCategory(0, "仙逆", 3)
	var rows []model.MovieMatchKey
	if err := gdb.Where("mid = ?", mid).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	kept := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		kept[row.MatchKey] = struct{}{}
	}
	for _, key := range stale {
		if _, ok := kept[key]; ok {
			t.Fatalf("stale key %s still stored", key)
		}
	}
}

func assertMatchKeys(t *testing.T, gdb *gorm.DB, mid int64, want []string) {
	t.Helper()
	var rows []model.MovieMatchKey
	if err := gdb.Where("mid = ?", mid).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	got := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		got[row.MatchKey] = struct{}{}
	}
	if len(got) != len(want) {
		t.Fatalf("keys=%d want=%d got=%v", len(got), len(want), got)
	}
	for _, key := range want {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing key %s in %v", key, got)
		}
	}
}

func TestPickPlaylistRemarks_IgnoresDisabledSources(t *testing.T) {
	sources := []model.FilmSource{
		{Id: "src-active", State: true},
		{Id: "src-disabled", State: false},
	}
	lines := []model.FilmSourcePlaylist{
		{SourceId: "src-active", EpisodeCount: 10, LastEpisode: "第10集"},
		{SourceId: "src-disabled", EpisodeCount: 15, LastEpisode: "第15集"},
	}
	if got := pickPlaylistRemarks(lines, sources); got != "第10集" {
		t.Fatalf("want active source remark '第10集', got %q", got)
	}
}
