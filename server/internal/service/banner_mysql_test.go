package service

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

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
		item, err := BannerSvc.Create("src1", BannerFields{Mid: i64Ptr(mid)})
		if err != nil {
			t.Fatalf("create %d: %v", mid, err)
		}
		ids = append(ids, item.Id)
	}
	if _, err := BannerSvc.Create("src1", BannerFields{Mid: i64Ptr(1)}); !IsBannerUserError(err) {
		t.Fatalf("duplicate film should be a user error, got %v", err)
	}

	// 影片 4 在本站没线路：首页跳过，后台标记不可用，排序照常。
	gdb.Where("mid = ? AND source_id = ?", 4, "src1").Delete(&model.FilmSourcePlaylist{})
	home, err := BannerSvc.HomeBanners("src1")
	if err != nil || len(home) != 3 {
		t.Fatalf("home should skip film without line on site: %v %+v", err, home)
	}
	view, err := BannerSvc.List("src1")
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
			_, errs[i] = BannerSvc.Reorder("src1", view.Revision, orders[i])
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
	cur, _ := BannerSvc.List("src1")
	same := make([]string, 0, len(cur.Items))
	for _, it := range cur.Items {
		same = append(same, it.Id)
	}
	if _, err := BannerSvc.Reorder("src1", cur.Revision, same); err != nil {
		t.Fatalf("reorder with unchanged order should succeed: %v", err)
	}

	// 并发发布整组：同样只能一个成功。
	cur, _ = BannerSvc.List("src1")
	decks := [][]BannerFields{{{Mid: i64Ptr(1)}}, {{Mid: i64Ptr(2)}, {Mid: i64Ptr(3)}}}
	for i := range decks {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = BannerSvc.PublishDeck("src1", cur.Revision, decks[i])
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
	seedBannerFilm(t, gdb, 9, "src2", 1, "站二", 1)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = BannerSvc.Create("src2", BannerFields{Mid: i64Ptr(9)})
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
