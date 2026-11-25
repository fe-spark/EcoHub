package banner

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"server/internal/film/catalog"
	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// setupBannerMySQL 设置 ECOHUB_TEST_MYSQL_DSN 时用真实 MySQL 跑轮播关键路径（会重建用到的表，多个包一起跑时加 -p 1），否则跳过。
func setupBannerMySQL(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("ECOHUB_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("ECOHUB_TEST_MYSQL_DSN 未设置，跳过 MySQL 测试")
	}
	gdb, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
		Logger:         logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}
	models := []any{
		&model.FilmIndex{}, &model.FilmSourcePlaylist{}, &model.FilmSource{}, &model.BannerSlot{},
		&model.SourceCategory{}, &model.Category{}, &model.CategoryMapping{}, &model.MappingRule{},
		&model.MovieMatchKey{}, &model.MovieSourceMapping{}, &model.FilmTMDBScrape{},
		&model.PrimaryState{},
	}
	_ = gdb.Migrator().DropTable(models...)
	if err := gdb.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	origMdb, origRdb := db.Mdb, db.Rdb
	db.Mdb, db.Rdb = gdb, nil
	t.Cleanup(func() { db.Mdb, db.Rdb = origMdb, origRdb })
	now := time.Now()
	if err := gdb.Create(&[]model.FilmSource{
		{Id: "src1", Name: "站点一", Uri: "http://src1.test", State: true, Sort: 1, CreatedAt: now},
		{Id: "src2", Name: "站点二", Uri: "http://src2.test", State: true, Sort: 2, CreatedAt: now},
	}).Error; err != nil {
		t.Fatal(err)
	}
	return gdb
}

func TestBannerMySQL_UnavailableAndConcurrentWrites(t *testing.T) {
	gdb := setupBannerMySQL(t)
	for mid := int64(1); mid <= 4; mid++ {
		seedBannerFilm(t, gdb, mid, "src1", 1, fmt.Sprintf("影片%d", mid), 10-mid)
	}
	ids := make([]string, 0, 4)
	for mid := int64(1); mid <= 4; mid++ {
		item, err := BannerSvc.Create("", BannerFields{Mid: i64Ptr(mid)})
		if err != nil {
			t.Fatalf("create %d: %v", mid, err)
		}
		ids = append(ids, item.Id)
	}
	if _, err := BannerSvc.Create("", BannerFields{Mid: i64Ptr(1)}); !IsBannerUserError(err) {
		t.Fatalf("duplicate film should be a user error, got %v", err)
	}

	// 影片 4 在本站没线路：首页跳过，后台标记不可用，排序照常。
	gdb.Where("mid = ? AND source_id = ?", 4, "src1").Delete(&model.FilmSourcePlaylist{})
	home, err := BannerSvc.HomeBanners("src1")
	if err != nil || len(home) != 3 {
		t.Fatalf("home should skip film without line on site: %v %+v", err, home)
	}
	view, err := BannerSvc.List("")
	if err != nil || len(view.Items) != 4 || !view.Items[3].Unavailable {
		t.Fatalf("admin list should mark unavailable: %v %+v", err, view.Items)
	}

	// 两个请求带同一个 revision 同时保存顺序：只能有一个成功，另一个 409。
	orders := [][]string{{ids[3], ids[2], ids[1], ids[0]}, {ids[1], ids[0], ids[3], ids[2]}}
	errs := make([]error, len(orders))
	var wg sync.WaitGroup
	for i := range orders {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = BannerSvc.Reorder("", view.Revision, orders[i])
		}(i)
	}
	wg.Wait()
	ok, conflict := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case IsBannerConflict(err):
			conflict++
		default:
			t.Fatalf("unexpected reorder error: %v", err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("concurrent reorder: want 1 ok + 1 conflict, got ok=%d conflict=%d", ok, conflict)
	}

	// 排序没变化的行 MySQL 返回 RowsAffected=0，不能误判为冲突。
	cur, _ := BannerSvc.List("")
	same := make([]string, 0, len(cur.Items))
	for _, it := range cur.Items {
		same = append(same, it.Id)
	}
	if _, err := BannerSvc.Reorder("", cur.Revision, same); err != nil {
		t.Fatalf("reorder with unchanged order should succeed: %v", err)
	}

	// 并发发布整组：同样只能一个成功。
	cur, _ = BannerSvc.List("")
	decks := [][]BannerFields{{{Mid: i64Ptr(1)}}, {{Mid: i64Ptr(2)}, {Mid: i64Ptr(3)}}}
	for i := range decks {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = BannerSvc.PublishDeck("", cur.Revision, decks[i])
		}(i)
	}
	wg.Wait()
	ok, conflict = 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case IsBannerConflict(err):
			conflict++
		default:
			t.Fatalf("unexpected deck error: %v", err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("concurrent deck: want 1 ok + 1 conflict, got ok=%d conflict=%d", ok, conflict)
	}

	// 并发新增同一部影片：一个成功，另一个是友好的重复错误。
	seedBannerFilm(t, gdb, 9, "src1", 1, "站九", 1)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = BannerSvc.Create("", BannerFields{Mid: i64Ptr(9)})
		}(i)
	}
	wg.Wait()
	ok = 0
	for _, err := range errs {
		if err == nil {
			ok++
		} else if !IsBannerUserError(err) {
			t.Fatalf("duplicate create should be a user error, got %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("concurrent create: want exactly 1 ok, got %d (%v)", ok, errs)
	}
}

func TestBannerMySQL_FallbackAndRandomExplainPlans(t *testing.T) {
	gdb := setupBannerMySQL(t)
	if err := gdb.Exec(
		"ALTER TABLE " + model.TableFilmIndex + " ADD INDEX " + catalog.FilmCidScoreIndex + " (cid, score, hits, mid)",
	).Error; err != nil {
		t.Fatalf("add cid score index: %v", err)
	}

	films := make([]model.FilmIndex, 0, 120)
	lines := make([]model.FilmSourcePlaylist, 0, 120)
	for mid := int64(1); mid <= 120; mid++ {
		films = append(films, model.FilmIndex{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: mid, FirstSourceId: "src1"},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 1 + (mid % 3), Cid: 10 + (mid % 4), CName: "动作片"},
			FilmIndexContent:  model.FilmIndexContent{Name: fmt.Sprintf("影片%d", mid), Hits: mid * 5, Score: 6.5, UpdateStamp: mid * 10},
		})
		lines = append(lines, model.FilmSourcePlaylist{Mid: mid, SourceId: "src2", LineKind: model.LineKindPlay})
	}
	if err := gdb.Create(&films).Error; err != nil {
		t.Fatalf("seed films: %v", err)
	}
	if err := gdb.Create(&lines).Error; err != nil {
		t.Fatalf("seed lines: %v", err)
	}
	_ = gdb.Exec("ANALYZE TABLE " + model.TableFilmIndex + ", " + model.TableFilmSourcePlaylist)

	type explainRow struct {
		Table string `gorm:"column:table"`
		Key   string `gorm:"column:key"`
		Extra string `gorm:"column:Extra"`
	}
	cases := []struct {
		name    string
		wantKey string
		sql     string
		args    []any
	}{
		{
			name:    "cid_latest",
			wantKey: "idx_cid_update",
			sql:     "EXPLAIN SELECT mid, hits, score, update_stamp FROM film_index USE INDEX (`idx_cid_update`) WHERE deleted_at IS NULL AND cid = ? AND EXISTS (SELECT 1 FROM film_source_playlist AS p FORCE INDEX (PRIMARY) WHERE p.mid = film_index.mid AND p.source_id = ? AND p.line_kind = ?) ORDER BY update_stamp DESC, mid DESC LIMIT ?",
			args:    []any{10, "src2", model.LineKindPlay, 5},
		},
		{
			name:    "cid_hot",
			wantKey: "idx_cid_hits",
			sql:     "EXPLAIN SELECT mid, hits, score, update_stamp FROM film_index USE INDEX (`idx_cid_hits`) WHERE deleted_at IS NULL AND cid = ? AND EXISTS (SELECT 1 FROM film_source_playlist AS p FORCE INDEX (PRIMARY) WHERE p.mid = film_index.mid AND p.source_id = ? AND p.line_kind = ?) ORDER BY hits DESC, mid DESC LIMIT ?",
			args:    []any{10, "src2", model.LineKindPlay, 5},
		},
		{
			name:    "cid_score",
			wantKey: catalog.FilmCidScoreIndex,
			sql:     "EXPLAIN SELECT mid, hits, score, update_stamp FROM film_index USE INDEX (`" + catalog.FilmCidScoreIndex + "`) WHERE deleted_at IS NULL AND cid = ? AND score >= ? AND EXISTS (SELECT 1 FROM film_source_playlist AS p FORCE INDEX (PRIMARY) WHERE p.mid = film_index.mid AND p.source_id = ? AND p.line_kind = ?) ORDER BY score DESC, hits DESC, mid DESC LIMIT ?",
			args:    []any{10, 6.0, "src2", model.LineKindPlay, 5},
		},
		{
			name:    "cid_random",
			wantKey: "idx_film_index_cid",
			sql:     "EXPLAIN SELECT mid, hits, score, update_stamp FROM film_index USE INDEX (`idx_film_index_cid`) WHERE deleted_at IS NULL AND cid = ? AND mid >= ? AND EXISTS (SELECT 1 FROM film_source_playlist AS p FORCE INDEX (PRIMARY) WHERE p.mid = film_index.mid AND p.source_id = ? AND p.line_kind = ?) ORDER BY mid ASC LIMIT ?",
			args:    []any{10, 50, "src2", model.LineKindPlay, 30},
		},
		{
			name:    "pid_random_unscoped",
			wantKey: "idx_film_index_pid",
			sql:     "EXPLAIN SELECT mid FROM `film_index` USE INDEX (`idx_film_index_pid`) WHERE `film_index`.`deleted_at` IS NULL AND pid IN (?) AND mid >= ? ORDER BY mid ASC LIMIT ?",
			args:    []any{1, 50, 30},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var rows []explainRow
			if err := gdb.Raw(tc.sql, tc.args...).Scan(&rows).Error; err != nil {
				t.Fatalf("explain failed: %v", err)
			}
			matched := false
			for _, r := range rows {
				if r.Table != model.TableFilmIndex {
					continue
				}
				matched = true
				if r.Key != tc.wantKey {
					t.Fatalf("expected index %q on film_index, got %q (extra=%q)", tc.wantKey, r.Key, r.Extra)
				}
				if strings.Contains(r.Extra, "Using filesort") {
					t.Fatalf("unexpected filesort on %s: key=%q extra=%q", tc.name, r.Key, r.Extra)
				}
			}
			if !matched {
				t.Fatalf("film_index row not found in explain output: %+v", rows)
			}
		})
	}
}
