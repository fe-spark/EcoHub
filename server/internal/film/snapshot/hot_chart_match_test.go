package snapshot

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"server/internal/film/catalog"
	"server/internal/film/shared"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/taxonomy"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupHotChartMatchDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:hot_chart_match_%d_%d?mode=memory&cache=shared", time.Now().UnixNano(), rand.Intn(100000))
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(
		&model.FilmIndex{},
		&model.FilmSourcePlaylist{},
		&model.MovieMatchKey{},
		&model.FilmSource{},
		&model.Category{},
		&model.SourceCategory{},
		&model.CategoryMapping{},
		&model.MappingRule{},
		&model.FilmTMDBScrape{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	origMdb, origRdb := db.Mdb, db.Rdb
	db.Mdb, db.Rdb = gdb, nil
	t.Cleanup(func() {
		db.Mdb, db.Rdb = origMdb, origRdb
		ClearActiveFilmReadModel()
		catalog.InvalidatePublicSourceCache()
	})
	return gdb
}

func seedMatchFilm(t *testing.T, gdb *gorm.DB, mid int64, sourceID string, pid, cid int64, dbID int64, name string, year, hits int64, playable bool) {
	t.Helper()
	row := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: mid, FirstSourceId: sourceID, DbId: dbID},
		FilmIndexCategory: model.FilmIndexCategory{
			Pid: pid, Cid: cid, CName: taxonomy.GetCategoryNameById(pid),
			RootCategoryKey: taxonomy.BuildSourceCategoryKey(sourceID, pid),
			CategoryKey:     taxonomy.BuildSourceCategoryKey(sourceID, cid),
		},
		FilmIndexContent: model.FilmIndexContent{
			Name: name, Year: year, Hits: hits, Score: 8.1,
			Picture: fmt.Sprintf("http://img/%d.jpg", mid),
		},
	}
	if err := gdb.Create(&row).Error; err != nil {
		t.Fatalf("seed film: %v", err)
	}
	if playable {
		if err := gdb.Create(&model.FilmSourcePlaylist{
			Mid: mid, SourceId: sourceID, LineKind: model.LineKindPlay, GroupIndex: 0, GroupName: "线路",
		}).Error; err != nil {
			t.Fatalf("seed line: %v", err)
		}
	}
	keys := shared.BuildMovieMatchKeysWithCategory(dbID, name, pid)
	if len(keys) > 0 {
		_ = shared.SaveMovieMatchKeysByMidTx(gdb, map[int64][]string{mid: keys})
	}
}

func TestMatchHotChartCandidates_DbIDTitleConflictAndVisibility(t *testing.T) {
	gdb := setupHotChartMatchDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "src1", Name: "主站", Uri: "http://src1.test", State: true, Sort: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&[]model.Category{
		{Id: 1, Pid: 0, Name: "电影", StableKey: "test-movie", Show: true, Sort: 1},
		{Id: 2, Pid: 0, Name: "连续剧", StableKey: "test-tv", Show: true, Sort: 2},
		{Id: 3, Pid: 0, Name: "隐藏分类", StableKey: "test-hidden", Show: false, Sort: 3},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(&model.Category{}).Where("id = ?", 3).Update("show", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&[]model.SourceCategory{
		{SourceId: "src1", SourceTypeId: 1, RawName: "电影", Sort: 1},
		{SourceId: "src1", SourceTypeId: 2, RawName: "连续剧", Sort: 2},
		{SourceId: "src1", SourceTypeId: 3, RawName: "隐藏分类", Sort: 3},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&[]model.CategoryMapping{
		{SourceId: "src1", SourceTypeId: 1, CategoryId: 1},
		{SourceId: "src1", SourceTypeId: 2, CategoryId: 2},
		{SourceId: "src1", SourceTypeId: 3, CategoryId: 3},
	}).Error; err != nil {
		t.Fatal(err)
	}
	taxonomy.RefreshCategoryCache()
	catalog.InvalidatePublicSourceCache()

	// 101: db_id 精确命中（即使站点不是 first_source_id 也能凭 src1 播放线命中）
	seedMatchFilm(t, gdb, 101, "src_other", 1, 1, 36915988, "饥饿游戏：收获日出", 2026, 50, true)
	if err := gdb.Create(&model.FilmSourcePlaylist{
		Mid: 101, SourceId: "src1", LineKind: model.LineKindPlay, GroupIndex: 1, GroupName: "线路2",
	}).Error; err != nil {
		t.Fatal(err)
	}
	// 102: 仅片名匹配且分类相符、年份在容差内
	seedMatchFilm(t, gdb, 102, "src1", 1, 1, 0, "沙丘2", 2024, 120, true)
	// 103: 同名但分类冲突（候选是 movie，库里是 连续剧）应被排除
	seedMatchFilm(t, gdb, 103, "src1", 2, 2, 0, "冲突片名", 2025, 200, true)
	// 104: 隐藏分类影片不应命中
	seedMatchFilm(t, gdb, 104, "src1", 3, 3, 999999, "隐藏影片", 2026, 300, true)
	// 105: 无 src1 播放线路不应命中
	seedMatchFilm(t, gdb, 105, "src1", 1, 1, 888888, "无线路影片", 2026, 300, false)

	cands := []HotChartCandidate{
		{DoubanID: 36915988, Title: "饥饿游戏：收获日出", Year: 2026, MediaKind: "movie"},
		{DoubanID: 0, Title: "沙丘2", Year: 2025, MediaKind: "movie"},
		{DoubanID: 0, Title: "冲突片名", Year: 2025, MediaKind: "movie"},
		{DoubanID: 999999, Title: "隐藏影片", Year: 2026, MediaKind: "movie"},
		{DoubanID: 888888, Title: "无线路影片", Year: 2026, MediaKind: "movie"},
	}

	mids, idxMap, err := MatchHotChartCandidates("live", "src1", cands, 10)
	if err != nil {
		t.Fatalf("MatchHotChartCandidates error: %v", err)
	}
	if len(mids) != 2 || mids[0] != 101 || mids[1] != 102 {
		t.Fatalf("unexpected matched mids: %v (map=%v)", mids, idxMap)
	}
	if idxMap[101] != 0 || idxMap[102] != 1 {
		t.Fatalf("unexpected midToCandIdx: %v", idxMap)
	}

	// exclude 参数生效
	midsExcl, _, err := MatchHotChartCandidates("live", "src1", cands, 10, 101)
	if err != nil {
		t.Fatalf("MatchHotChartCandidates exclude error: %v", err)
	}
	if len(midsExcl) != 1 || midsExcl[0] != 102 {
		t.Fatalf("expected [102] when 101 excluded, got %v", midsExcl)
	}
}
