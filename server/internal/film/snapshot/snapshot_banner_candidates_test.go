package snapshot

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"server/internal/model"
	"server/internal/taxonomy"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
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
		snaps, err := GetSnapshotBannerCandidates(context.Background(), "live", strategy, "", []int64{1}, limit, exclude)
		if err != nil {
			t.Fatalf("candidates: %v", err)
		}
		got := bannerCandidateMids(snaps)
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
	if got, _ := GetSnapshotBannerCandidates(context.Background(), "live", "hot_random", "", []int64{1}, 0, nil); len(got) != 0 {
		t.Fatalf("limit 0 got %v", bannerCandidateMids(got))
	}
}

func TestGetSnapshotBannerCandidates_SourceUsesCategoryKeyAndPlayLine(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	const sourceID = "banner_src"
	key := "source:banner_src:1"
	other := "source:banner_src:2"
	rows := []model.FilmIndex{
		bannerCandidateFilm(1, 1, 10, 9.0, 100, "高分有线路"),
		bannerCandidateFilm(2, 1, 80, 4.0, 200, "最热无线路"),
		bannerCandidateFilm(3, 1, 40, 4.0, 300, "次热有线路"),
		bannerCandidateFilm(4, 2, 100, 9.5, 400, "他类更热"),
	}
	rows[0].CategoryKey = key
	rows[1].CategoryKey = key
	rows[2].CategoryKey = key
	rows[3].CategoryKey = other
	if err := gdb.Create(&rows).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	lines := []model.FilmSourcePlaylist{
		{Mid: 1, SourceId: sourceID, LineKind: model.LineKindPlay},
		{Mid: 3, SourceId: sourceID, LineKind: model.LineKindPlay},
		{Mid: 4, SourceId: sourceID, LineKind: model.LineKindPlay},
	}
	if err := gdb.Create(&lines).Error; err != nil {
		t.Fatalf("seed playlist: %v", err)
	}

	hot, err := GetSnapshotBannerCandidates(context.Background(), "live", "hot_random", sourceID, []int64{1}, 2, nil)
	if err != nil {
		t.Fatalf("hot: %v", err)
	}
	if got := bannerCandidateMids(hot); len(got) != 2 || got[0] != 3 || got[1] != 1 {
		t.Fatalf("hot got %v", got)
	}
	score, err := GetSnapshotBannerCandidates(context.Background(), "live", "score_random", sourceID, []int64{1}, 2, nil)
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if got := bannerCandidateMids(score); len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("score got %v", got)
	}
}

func TestGetSnapshotBannerCandidates_CanceledContext(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	if err := gdb.Create(&[]model.FilmIndex{bannerCandidateFilm(1, 1, 10, 9.0, 100, "影片")}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := GetSnapshotBannerCandidates(ctx, "live", "hot_random", "", []int64{1}, 2, nil); err == nil {
		t.Fatalf("canceled context should return an error")
	}
}

func TestGetSnapshotBannerCandidates_ExcludesHiddenSubcategory(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	taxonomy.SetCategoryTreeForTest(map[int64]int64{1: 0, 10: 1, 19: 1}, map[int64]string{1: "电影", 10: "动作片", 19: "伦理片"})
	taxonomy.SetCategoryShowForTest(map[int64]bool{1: true, 10: true, 19: false})
	t.Cleanup(func() {
		taxonomy.SetCategoryShowForTest(nil)
		taxonomy.SetCategoryTreeForTest(nil, nil)
	})

	visible := bannerCandidateFilm(1, 1, 50, 8.0, 200, "公开动作")
	visible.Cid = 10
	hidden := bannerCandidateFilm(2, 1, 999, 9.5, 999, "隐藏伦理")
	hidden.Cid = 19
	if err := gdb.Create(&[]model.FilmIndex{visible, hidden}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	gotSelected, err := GetSnapshotBannerCandidates(context.Background(), "live", "hot_random", "", []int64{1}, 5, nil)
	if err != nil {
		t.Fatalf("selected candidates: %v", err)
	}
	if mids := bannerCandidateMids(gotSelected); len(mids) != 1 || mids[0] != 1 {
		t.Fatalf("selected banner leaked hidden subcategory: %v", mids)
	}

	gotAll, err := GetSnapshotBannerCandidates(context.Background(), "live", "hot_random", "", nil, 5, nil)
	if err != nil {
		t.Fatalf("all candidates: %v", err)
	}
	if mids := bannerCandidateMids(gotAll); len(mids) != 1 || mids[0] != 1 {
		t.Fatalf("unbounded banner leaked hidden subcategory: %v", mids)
	}
}

func TestScanBannerFallbackRanks_SmallSourceAndBranchUnion(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	var rows []model.FilmIndex
	var lines []model.FilmSourcePlaylist
	for mid := int64(1); mid <= 4; mid++ {
		f := bannerCandidateFilm(mid, 1, mid*10, float64(4+mid), mid*100, "大片")
		f.Cid = 10 + (mid % 2)
		f.FirstSourceId = "other"
		rows = append(rows, f)
		lines = append(lines, model.FilmSourcePlaylist{Mid: mid, SourceId: "small_src", LineKind: model.LineKindPlay})
	}
	if err := gdb.Create(&rows).Error; err != nil {
		t.Fatalf("seed rows: %v", err)
	}
	if err := gdb.Create(&lines).Error; err != nil {
		t.Fatalf("seed small lines: %v", err)
	}

	// 小站路径（<= smallSourceFallbackCap）
	smallHot, err := scanBannerFallbackRanks(context.Background(), "hot_random", "small_src", []int64{10, 11}, 2, []int64{4}, false)
	if err != nil {
		t.Fatalf("small fallback: %v", err)
	}
	smallPicked := takeBannerRanks("hot_random", smallHot, 2)
	if len(smallPicked) != 2 || smallPicked[0].Mid != 3 || smallPicked[1].Mid != 2 {
		t.Fatalf("small fallback got %+v", smallPicked)
	}

	// 大站路径（> smallSourceFallbackCap）：直接走按单 cid 拆支 UNION ALL 分支，验证三类策略与排除均正确
	bigLines := make([]model.FilmSourcePlaylist, 0, smallSourceFallbackCap+1)
	for i := 0; i <= smallSourceFallbackCap; i++ {
		bigLines = append(bigLines, model.FilmSourcePlaylist{
			Mid:      int64(10000 + i),
			SourceId: "big_src",
			LineKind: model.LineKindPlay,
		})
	}
	for mid := int64(1); mid <= 4; mid++ {
		bigLines = append(bigLines, model.FilmSourcePlaylist{
			Mid:      mid,
			SourceId: "big_src",
			LineKind: model.LineKindPlay,
		})
	}
	if err := gdb.CreateInBatches(&bigLines, 500).Error; err != nil {
		t.Fatalf("seed big lines: %v", err)
	}

	bigHot, err := scanBannerBranches(context.Background(), "hot_random", "big_src", "cid", []int64{10, 11}, true, 2, []int64{4}, false)
	if err != nil {
		t.Fatalf("big hot: %v", err)
	}
	hotPicked := takeBannerRanks("hot_random", bigHot, 2)
	if len(hotPicked) != 2 || hotPicked[0].Mid != 3 || hotPicked[1].Mid != 2 {
		t.Fatalf("big hot got %+v", hotPicked)
	}

	bigScore, err := scanBannerFallbackRanks(context.Background(), "score_random", "big_src", []int64{10, 11}, 3, nil, true)
	if err != nil {
		t.Fatalf("big score: %v", err)
	}
	scorePicked := takeBannerRanks("score_random", bigScore, 3)
	// mid=4(8.0), mid=3(7.0), mid=2(6.0); mid=1 score=5.0<6.0
	if len(scorePicked) != 3 || scorePicked[0].Mid != 4 || scorePicked[1].Mid != 3 || scorePicked[2].Mid != 2 {
		t.Fatalf("big score got %+v", scorePicked)
	}

	// 随机策略：验证不限制站点、小站兜底、大站拆支均正常返回且不重复、遵守排除名单。
	unscopedRandom, err := GetSnapshotBannerCandidates(context.Background(), "live", "random", "", []int64{1}, 3, []int64{4})
	if err != nil {
		t.Fatalf("unscoped random: %v", err)
	}
	if len(unscopedRandom) != 3 {
		t.Fatalf("unscoped random len = %d", len(unscopedRandom))
	}
	seen := make(map[int64]bool, 3)
	for _, snap := range unscopedRandom {
		if snap.Mid == 4 || seen[snap.Mid] {
			t.Fatalf("unexpected/duplicate mid in unscoped random: %v", bannerCandidateMids(unscopedRandom))
		}
		seen[snap.Mid] = true
	}

	smallRandom, err := scanBannerFallbackRanks(context.Background(), "random", "small_src", []int64{10, 11}, 2, []int64{4}, false)
	if err != nil {
		t.Fatalf("small random: %v", err)
	}
	if got := takeBannerRanks("random", smallRandom, 2); len(got) != 2 {
		t.Fatalf("small random picked %+v", got)
	}

	bigRandom, err := scanBannerBranches(context.Background(), "random", "big_src", "cid", []int64{10, 11}, true, 2, []int64{4}, false)
	if err != nil {
		t.Fatalf("big random: %v", err)
	}
	if got := takeBannerRanks("random", bigRandom, 2); len(got) != 2 {
		t.Fatalf("big random picked %+v", got)
	}
}

func TestFilmIndexSchema_CidCompositeIndexOrder(t *testing.T) {
	s, err := schema.Parse(&model.FilmIndex{}, &sync.Map{}, schema.NamingStrategy{SingularTable: true})
	if err != nil {
		t.Fatalf("parse FilmIndex schema: %v", err)
	}
	byName := make(map[string][]string)
	for _, idx := range s.ParseIndexes() {
		cols := make([]string, 0, len(idx.Fields))
		for _, f := range idx.Fields {
			cols = append(cols, f.DBName)
		}
		byName[idx.Name] = cols
	}

	wantIndexes := map[string][]string{
		bannerIndexCidUpdate: {"cid", "update_stamp", "mid"},
		bannerIndexCidHits:   {"cid", "hits", "mid"},
		bannerIndexCid:       {"cid"},
		bannerIndexPid:       {"pid"},
	}
	for name, want := range wantIndexes {
		got, ok := byName[name]
		if !ok {
			t.Fatalf("missing index %s in FilmIndex schema", name)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("index %s columns = %v, want %v", name, got, want)
		}
	}
}

func TestBannerMySQLSQLPlans_CidFallbackAndRandomIndexHints(t *testing.T) {
	gdb, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(127.0.0.1:3306)/gorm?charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
		Logger:               logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open mysql dry-run: %v", err)
	}

	cases := []struct {
		strategy  string
		extra     string
		wantIndex string
		wantOrder string
	}{
		{
			strategy:  "latest_random",
			wantIndex: "USE INDEX (`idx_cid_update`)",
			wantOrder: "ORDER BY update_stamp DESC, mid DESC LIMIT ?",
		},
		{
			strategy:  "hot_random",
			wantIndex: "USE INDEX (`idx_cid_hits`)",
			wantOrder: "ORDER BY hits DESC, mid DESC LIMIT ?",
		},
		{
			strategy:  "random",
			extra:     " AND mid >= ?",
			wantIndex: "USE INDEX (`idx_film_index_cid`)",
			wantOrder: "ORDER BY mid ASC LIMIT ?",
		},
	}
	for _, tc := range cases {
		hint := bannerUseIndexClause(gdb, "mysql", tc.strategy, true)
		_, order := bannerStrategyPlan(tc.strategy, true)
		sql := bannerBranchSQL("mysql", hint, "cid", order, false, tc.extra, []int64{99})
		if !strings.Contains(sql, tc.wantIndex) {
			t.Fatalf("strategy %s missing index hint %q in SQL: %s", tc.strategy, tc.wantIndex, sql)
		}
		if !strings.Contains(sql, "FORCE INDEX (PRIMARY)") {
			t.Fatalf("strategy %s missing playlist FORCE INDEX (PRIMARY) in SQL: %s", tc.strategy, sql)
		}
		if !strings.Contains(sql, tc.wantOrder) {
			t.Fatalf("strategy %s missing order %q in SQL: %s", tc.strategy, tc.wantOrder, sql)
		}
	}

	scopedSQL := gdb.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []model.FilmIndex
		base := applyBannerCategories(tx.Model(&model.FilmIndex{}), "", []int64{1})
		return buildRandomBannerRangeQuery(base, true, ">=", 500, 30).Find(&rows)
	})
	if !strings.Contains(scopedSQL, "USE INDEX (`idx_film_index_pid`)") {
		t.Fatalf("scoped random banner query must bind idx_film_index_pid, got: %s", scopedSQL)
	}
	if strings.Contains(scopedSQL, "`film_index USE INDEX") {
		t.Fatalf("table name must not swallow USE INDEX, got: %s", scopedSQL)
	}

	unboundedSQL := gdb.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []model.FilmIndex
		base := applyBannerCategories(tx.Model(&model.FilmIndex{}), "", nil)
		return buildRandomBannerRangeQuery(base, false, ">=", 500, 30).Find(&rows)
	})
	if strings.Contains(unboundedSQL, "idx_film_index_pid") {
		t.Fatalf("unbounded random banner query should not force pid index, got: %s", unboundedSQL)
	}
}
