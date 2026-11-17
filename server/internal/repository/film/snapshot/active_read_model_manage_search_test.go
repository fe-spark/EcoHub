package snapshot

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	"server/internal/utils"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestManageMetaCategory_SourceKey(t *testing.T) {
	detachGlobalDB(t)
	s := model.SearchVo{SourceId: "1208629981", Pid: 57}
	ctx, ok := newManageFilterContext(s)
	if !ok {
		t.Fatal("expected filter context to be created")
	}

	meta := FilmSearchMeta{Pid: 57, CategoryKey: "source:1208629981:57", PlaySources: []string{"1208629981"}}
	if !ctx.pass(&meta) {
		t.Fatal("source type key should match category_key")
	}
	byPidOnly := FilmSearchMeta{Pid: 57, CategoryKey: "source:other:57", PlaySources: []string{"1208629981"}}
	if ctx.pass(&byPidOnly) {
		t.Fatal("local pid must not match another source key")
	}

	sCid := model.SearchVo{SourceId: "1208629981", Cid: 58}
	ctxCid, ok := newManageFilterContext(sCid)
	if !ok {
		t.Fatal("expected cid filter context to be created")
	}
	cid := FilmSearchMeta{Cid: 58, CategoryKey: "source:1208629981:58", PlaySources: []string{"1208629981"}}
	if !ctxCid.pass(&cid) {
		t.Fatal("cid source key should match")
	}
}

func TestPageManageMetas_ScalePerformance(t *testing.T) {
	detachGlobalDB(t)
	const count = 50000
	items := make([]FilmSearchMeta, count)
	byUpdate := make([]int, count)
	for i := 0; i < count; i++ {
		byUpdate[i] = i
		items[i] = FilmSearchMeta{
			Mid:         int64(i + 1),
			Pid:         int64(i%5 + 1),
			Cid:         int64(i%20 + 1),
			PlaySources: []string{"3370810636"},
			CategoryKey: "source:3370810636:2",
		}
	}
	ptrs := make([]*FilmSearchMeta, len(items))
	for i := range items {
		ptrs[i] = &items[i]
	}
	idx := &filmSearchMetaIndex{
		Version:  "v_bench",
		Items:    ptrs,
		ByUpdate: byUpdate,
	}

	page := &dto.Page{Current: 1, PageSize: 10}
	s := model.SearchVo{
		SourceId: "3370810636",
		Pid:      2,
		Cid:      0,
	}

	// 50,000 条数据内存切页应在 50ms 内完成，绝对不能出现 18s 级卡顿
	start := time.Now()
	res, total, err := pageManageMetas(idx, s, page)
	cost := time.Since(start)
	if err != nil {
		t.Fatalf("pageManageMetas error: %v", err)
	}
	if cost > 100*time.Millisecond {
		t.Fatalf("pageManageMetas took too long: %s (must be < 100ms)", cost)
	}
	if len(res) != 10 {
		t.Fatalf("expected 10 items in window, got %d", len(res))
	}
	if total <= 0 {
		t.Fatalf("expected positive total, got %d", total)
	}
}

// detachGlobalDB 让用例在"未连接数据库"的前提下运行，结束后还原。
// 这些用例只测纯内存逻辑，但被测函数会读全局 db.Mdb；包内其他用例可能把 db.Mdb 留成已关闭/缺表的库，
// -count>1 时第二轮起就会读到它而失败。
func TestManageNonPrimarySearchUsesScoreAndScrape(t *testing.T) {
	prevBatch := manageScoreScanBatch
	manageScoreScanBatch = 1
	t.Cleanup(func() { manageScoreScanBatch = prevBatch })
	prev := db.Mdb
	t.Cleanup(func() { db.Mdb = prev })
	gdb := setupSearchSourceTestDB(t)
	if err := gdb.AutoMigrate(&model.FilmTMDBScrape{}, &model.MovieSourceMapping{}); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{
		Id: "hd", Name: "其他站", Uri: "https://example.com/hd", State: true, Sort: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	films := []model.FilmIndex{
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, FirstSourceId: "hd"},
			FilmIndexContent:  model.FilmIndexContent{Name: "修仙", Hits: 1},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 3, FirstSourceId: "hd"},
			FilmIndexContent:  model.FilmIndexContent{Name: "凡人修仙传", Hits: 99999},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 4, FirstSourceId: "hd"},
			FilmIndexContent:  model.FilmIndexContent{Name: "慶餘年２", Hits: 10},
		},
	}
	for _, row := range films {
		if err := gdb.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, mid := range []int64{2, 3, 4} {
		if err := gdb.Create(&model.FilmSourcePlaylist{Mid: mid, SourceId: "hd", LineKind: "play"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := gdb.Create(&model.FilmTMDBScrape{Mid: 3, TmdbID: 42, MediaType: "tv"}).Error; err != nil {
		t.Fatal(err)
	}

	page := &dto.Page{Current: 1, PageSize: 10}
	scored, err := GetSearchPageReadModel(model.SearchVo{SourceId: "hd", Name: "修仙", Paging: page})
	if err != nil {
		t.Fatal(err)
	}
	if len(scored) != 2 || scored[0].Mid != 2 || scored[1].Mid != 3 {
		t.Fatalf("score order = %+v", scored)
	}
	if !scored[1].IsScraped || scored[1].TmdbID != 42 {
		t.Fatalf("scrape flag = scraped:%v tmdb:%d", scored[1].IsScraped, scored[1].TmdbID)
	}

	skipped, err := GetSearchPageReadModel(model.SearchVo{SourceId: "hd", Name: "凡人传", Paging: page})
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 1 || skipped[0].Mid != 3 || !skipped[0].IsScraped {
		t.Fatalf("skip-char search = %+v", skipped)
	}

	trad, err := GetSearchPageReadModel(model.SearchVo{SourceId: "hd", Name: "庆余年2", Paging: page})
	if err != nil {
		t.Fatal(err)
	}
	if len(trad) != 1 || trad[0].Mid != 4 {
		t.Fatalf("traditional search = %+v", trad)
	}
}

func TestManageNonPrimaryListReadsOwnedFilmsByUpdate(t *testing.T) {
	gdb := setupSearchSourceTestDB(t)
	if err := gdb.AutoMigrate(&model.MovieSourceMapping{}); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{
		Id: "hd", Name: "其他站", Uri: "https://example.com/hd", State: true, Sort: 1, FilmCount: 2,
	}).Error; err != nil {
		t.Fatal(err)
	}
	films := []model.FilmIndex{
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, FirstSourceId: "hd"}, FilmIndexContent: model.FilmIndexContent{Name: "旧片", UpdateStamp: 100}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 3, FirstSourceId: "hd"}, FilmIndexContent: model.FilmIndexContent{Name: "新片", UpdateStamp: 300}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 4, FirstSourceId: "uku"}, FilmIndexContent: model.FilmIndexContent{Name: "主站片", UpdateStamp: 999}},
	}
	for _, row := range films {
		if err := gdb.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		if err := gdb.Create(&model.FilmSourcePlaylist{Mid: row.Mid, SourceId: "hd", LineKind: "play"}).Error; err != nil {
			t.Fatal(err)
		}
	}

	page := &dto.Page{Current: 1, PageSize: 10}
	list, err := GetSearchPageReadModel(model.SearchVo{SourceId: "hd", Paging: page})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(list) != 2 || list[0].Mid != 3 || list[1].Mid != 2 {
		t.Fatalf("list=%+v total=%d", list, page.Total)
	}
}

func TestUpsertNonPrimaryDoesNotResealIndex(t *testing.T) {
	prevDB := db.Mdb
	prevIdx := activeFilmSearchMetas.Load()
	t.Cleanup(func() {
		db.Mdb = prevDB
		activeFilmSearchMetas.Store(prevIdx)
	})
	gdb := setupSearchSourceTestDB(t)
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 9, FirstSourceId: "hd"},
		FilmIndexContent:  model.FilmIndexContent{Name: "其他站影片"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	item := FilmSearchMeta{Mid: 1, FirstSourceId: "uku", Item: utils.FilmSearchItem{Mid: 1, Name: "主站片"}}
	idx := sealSearchMetaIndex("v-keep", []FilmSearchMeta{item})
	idx.PrimaryID = "uku"
	activeFilmSearchMetas.Store(idx)

	UpsertMidsToActiveFilmSearchIndex("v-keep", []int64{9})
	if got := activeFilmSearchMetas.Load(); got != idx {
		t.Fatal("non-primary save resealed the resident index")
	}

	prevModel := activeFilmReadModel.Load()
	activeFilmReadModel.Store(&FilmReadModel{Version: "v-keep"})
	t.Cleanup(func() { activeFilmReadModel.Store(prevModel) })
	if _, _, err := UpsertActiveSnapshotsByMids(9); err != nil {
		t.Fatal(err)
	}
	if got := activeFilmSearchMetas.Load(); got != idx {
		t.Fatal("non-primary snapshot save wrote the search index")
	}
}

type sqlPrintfWriter struct {
	lines []string
}

func (w *sqlPrintfWriter) Printf(format string, args ...any) {
	w.lines = append(w.lines, fmt.Sprintf(format, args...))
}

func TestPageOrderedManageFilmsUsesSourceUpdateIndex(t *testing.T) {
	writer := &sqlPrintfWriter{}
	gdb, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(127.0.0.1:3306)/gorm?charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
		Logger: logger.New(writer, logger.Config{
			LogLevel: logger.Info,
		}),
	})
	if err != nil {
		t.Fatalf("open mysql dry-run: %v", err)
	}
	page := &dto.Page{Current: 1, PageSize: 10}
	q := gdb.Model(&model.FilmIndex{}).Where("first_source_id = ?", "hd")
	if _, err := pageOrderedManageFilms(q, page, "update_stamp DESC, mid DESC", 100, "hd"); err != nil {
		t.Fatal(err)
	}
	sql := strings.Join(writer.lines, "\n")
	want := "USE INDEX (`" + repository.SourceFilmListIndex + "`)"
	if !strings.Contains(sql, want) {
		t.Fatalf("sql=%s", sql)
	}
	if strings.Contains(sql, "`film_index USE INDEX") {
		t.Fatalf("sql=%s", sql)
	}
}

func TestApplyManageScoreIndexHint_MySQLUsesSourceUpdateIndex(t *testing.T) {
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
	sql := gdb.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []model.FilmIndex
		q := applyManageScoreIndexHint(tx.Model(&model.FilmIndex{}).Where("first_source_id = ?", "hd"), "hd")
		return q.Order("update_stamp DESC, mid DESC").Limit(14).Find(&rows)
	})
	want := "USE INDEX (`" + repository.SourceFilmListIndex + "`)"
	if !strings.Contains(sql, want) {
		t.Fatalf("sql=%s", sql)
	}
	if strings.Contains(sql, "`film_index USE INDEX") {
		t.Fatalf("sql=%s", sql)
	}
}

func detachGlobalDB(t *testing.T) {
	t.Helper()
	prev := db.Mdb
	db.Mdb = nil
	t.Cleanup(func() { db.Mdb = prev })
}
