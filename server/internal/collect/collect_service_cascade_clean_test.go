package collect

import (
	"server/internal/repobase"
	"sync"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/collect/progress"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestEnqueueDelFilmSources_ConcurrentBatchesAreAtomic(t *testing.T) {
	if !WaitAsyncWork(5 * time.Second) {
		t.Fatal("previous async work did not finish")
	}
	batches := [][]*model.FilmSource{
		{
			{Id: "src_atomic_a1", Name: "站点A1"},
			{Id: "src_atomic_a2", Name: "站点A2"},
		},
		{
			{Id: "src_atomic_b1", Name: "站点B1"},
			{Id: "src_atomic_b2", Name: "站点B2"},
		},
	}
	queue := make(chan deleteSourceJob, 2)
	t.Cleanup(func() {
		for len(queue) > 0 {
			<-queue
			deleteSourceJobs.Done()
		}
		for _, batch := range batches {
			for _, src := range batch {
				progress.SetSourceDeleting(src.Id, false)
			}
		}
	})

	start := make(chan struct{})
	results := make([]error, len(batches))
	var wg sync.WaitGroup
	for i, batch := range batches {
		wg.Add(1)
		go func(i int, batch []*model.FilmSource) {
			defer wg.Done()
			<-start
			results[i] = enqueueDelFilmSources(queue, batch)
		}(i, batch)
	}
	close(start)
	wg.Wait()

	success := -1
	for i, err := range results {
		if err == nil {
			if success >= 0 {
				t.Fatal("both batches unexpectedly succeeded")
			}
			success = i
		}
	}
	if success < 0 {
		t.Fatal("neither batch succeeded")
	}
	if len(queue) != len(batches[success]) {
		t.Fatalf("queued %d sources, want whole batch of %d", len(queue), len(batches[success]))
	}
	queued := make(map[string]struct{}, len(batches[success]))
	drained := make([]deleteSourceJob, 0, len(batches[success]))
	for len(queue) > 0 {
		job := <-queue
		queued[job.id] = struct{}{}
		drained = append(drained, job)
	}
	for _, job := range drained {
		queue <- job
	}
	for _, src := range batches[success] {
		if _, ok := queued[src.Id]; !ok {
			t.Fatalf("queued batch does not contain %s", src.Id)
		}
		if !progress.IsSourceDeleting(src.Id) {
			t.Fatalf("accepted source %s was not marked deleting", src.Id)
		}
	}
	for i, batch := range batches {
		if i == success {
			continue
		}
		if results[i] == nil {
			t.Fatal("rejected batch unexpectedly succeeded")
		}
		for _, src := range batch {
			if progress.IsSourceDeleting(src.Id) {
				t.Fatalf("rejected source %s was marked deleting", src.Id)
			}
		}
	}
}

func setupCascadeCleanTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := gdb.AutoMigrate(
		&model.FilmSource{},
		&model.FilmIndex{},
		&model.FilmSourcePlaylist{},
		&model.MovieMatchKey{},
		&model.MovieSourceMapping{},
		&model.BannerSlot{},
		&model.FilmTMDBScrape{},
		&model.SourceCategory{},
		&model.CategoryMapping{},
		&model.Category{},
		&model.CollectRecord{},
		&model.CollectRecordFilmItem{},
		&model.CollectSourceStats{},
		&model.PrimaryState{},
	); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	// 删站会触发切主站的异步预热，它读全局 db；等它结束再换库，避免 -race 报竞争。
	waitSearchWorkIdle(t)
	prevMdb, prevRdb := db.Mdb, db.Rdb
	db.Mdb = gdb
	db.Rdb = nil
	t.Cleanup(func() {
		waitSearchWorkIdle(t)
		db.Mdb, db.Rdb = prevMdb, prevRdb
	})
	return gdb
}

func TestCollectService_DelFilmSource_CleansOrphanFilms(t *testing.T) {
	gdb := setupCascadeCleanTestDB(t)

	// 创建两个采集源: src1 和 src2
	gdb.Create(&model.FilmSource{Id: "src1", Name: "源1", Uri: "http://src1.com", State: true})
	gdb.Create(&model.FilmSource{Id: "src2", Name: "源2", Uri: "http://src2.com", State: true})

	// 影片 1、3 属于 src1。3 没有线路，也要删。影片 2 属于 src2。
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, FirstSourceId: "src1"},
		FilmIndexContent:  model.FilmIndexContent{Name: "源1影片"},
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, FirstSourceId: "src2"},
		FilmIndexContent:  model.FilmIndexContent{Name: "源2影片"},
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 3, FirstSourceId: "src1"},
		FilmIndexContent:  model.FilmIndexContent{Name: "源1无线路"},
	})

	gdb.Create(&model.FilmSourcePlaylist{Mid: 1, SourceId: "src1", LineKind: "play", Content: "[]"})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 2, SourceId: "src1", LineKind: "play", Content: "[]"})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 2, SourceId: "src2", LineKind: "play", Content: "[]"})
	gdb.Create(&model.MovieMatchKey{Mid: 1, MatchKey: "k1"})
	gdb.Create(&model.MovieMatchKey{Mid: 3, MatchKey: "k3"})
	gdb.Create(&model.MovieSourceMapping{SourceId: "src1", SourceMid: 1, GlobalMid: 1})
	gdb.Create(&model.BannerSlot{Id: "b1", SourceId: "src1", Mid: 1})
	gdb.Create(&model.FilmTMDBScrape{Mid: 1, TmdbID: 11, MediaType: "movie"})
	gdb.Create(&model.FilmTMDBScrape{Mid: 2, TmdbID: 22, MediaType: "movie"})
	gdb.Create(&model.SourceCategory{SourceId: "src1", SourceTypeId: 1, RawName: "电影"})
	gdb.Create(&model.SourceCategory{SourceId: "src2", SourceTypeId: 1, RawName: "电影"})
	gdb.Create(&model.CategoryMapping{SourceId: "src1", SourceTypeId: 1, CategoryId: 9})
	gdb.Create(&model.CategoryMapping{SourceId: "src2", SourceTypeId: 1, CategoryId: 9})

	// 删除 src1
	srv := new(CollectService)
	if _, err := srv.DelFilmSource("src1"); err != nil {
		t.Fatalf("DelFilmSource failed: %v", err)
	}

	var count1 int64
	gdb.Model(&model.FilmIndex{}).Where("mid = ?", 1).Count(&count1)
	if count1 != 0 {
		t.Fatalf("expected film 1 to be deleted, got count=%d", count1)
	}
	var count3 int64
	gdb.Model(&model.FilmIndex{}).Where("mid = ?", 3).Count(&count3)
	if count3 != 0 {
		t.Fatalf("expected playless film 3 to be deleted, got count=%d", count3)
	}
	var count2 int64
	gdb.Model(&model.FilmIndex{}).Where("mid = ?", 2).Count(&count2)
	if count2 != 1 {
		t.Fatalf("expected src2 film 2 to be kept, got count=%d", count2)
	}
	var lineCount int64
	gdb.Model(&model.FilmSourcePlaylist{}).Where("mid = ? AND source_id = ?", 2, "src2").Count(&lineCount)
	if lineCount != 1 {
		t.Fatalf("expected src2 playlist to remain, got %d", lineCount)
	}
	var src1Lines int64
	gdb.Model(&model.FilmSourcePlaylist{}).Where("source_id = ?", "src1").Count(&src1Lines)
	if src1Lines != 0 {
		t.Fatalf("expected src1 playlists to be deleted, got %d", src1Lines)
	}
	var keys int64
	var maps int64
	var banners int64
	var scrapes int64
	gdb.Model(&model.MovieMatchKey{}).Where("mid IN ?", []int64{1, 3}).Count(&keys)
	gdb.Model(&model.MovieSourceMapping{}).Where("source_id = ?", "src1").Count(&maps)
	gdb.Model(&model.BannerSlot{}).Where("mid = ?", 1).Count(&banners)
	gdb.Model(&model.FilmTMDBScrape{}).Where("mid = ?", 1).Count(&scrapes)
	if keys != 0 || maps != 0 || banners != 0 || scrapes != 0 {
		t.Fatalf("residue keys=%d maps=%d banners=%d scrapes=%d", keys, maps, banners, scrapes)
	}
	var src2Scrape int64
	gdb.Model(&model.FilmTMDBScrape{}).Where("mid = ?", 2).Count(&src2Scrape)
	if src2Scrape != 1 {
		t.Fatalf("expected src2 scrape to remain, got %d", src2Scrape)
	}
	var src1Raw int64
	var src1Map int64
	gdb.Model(&model.SourceCategory{}).Where("source_id = ?", "src1").Count(&src1Raw)
	gdb.Model(&model.CategoryMapping{}).Where("source_id = ?", "src1").Count(&src1Map)
	if src1Raw != 0 || src1Map != 0 {
		t.Fatalf("expected src1 categories to be deleted, raw=%d map=%d", src1Raw, src1Map)
	}
	var src2Raw int64
	var src2Map int64
	gdb.Model(&model.SourceCategory{}).Where("source_id = ?", "src2").Count(&src2Raw)
	gdb.Model(&model.CategoryMapping{}).Where("source_id = ?", "src2").Count(&src2Map)
	if src2Raw != 1 || src2Map != 1 {
		t.Fatalf("expected src2 category copy to remain, raw=%d map=%d", src2Raw, src2Map)
	}
}

func TestCollectService_DelFilmSource_RejectsWhileRunning(t *testing.T) {
	gdb := setupCascadeCleanTestDB(t)
	gdb.Create(&model.FilmSource{Id: "src1", Name: "源1", Uri: "http://src1.com", State: true, Sort: 0})
	gdb.Create(&model.FilmSource{Id: "src2", Name: "源2", Uri: "http://src2.com", State: true, Sort: 1})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, FirstSourceId: "src1"},
		FilmIndexContent:  model.FilmIndexContent{Name: "独占影片"},
	})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 1, SourceId: "src1", LineKind: "play", Content: "[]"})
	gdb.Create(&model.SourceCategory{SourceId: "src1", SourceTypeId: 1, RawName: "电影"})
	gdb.Create(&model.CategoryMapping{SourceId: "src1", SourceTypeId: 1, CategoryId: 9})
	progress.MarkSourcesCollectStarting([]model.FilmSource{{Id: "src1", Name: "源1"}, {Id: "src2", Name: "源2"}}, "q-del")
	t.Cleanup(func() {
		progress.MarkStopped("src1")
		progress.MarkStopped("src2")
	})

	_, err := new(CollectService).DelFilmSource("src1")
	if err == nil || err.Error() != "采集中，暂不可删除" {
		t.Fatalf("err = %v", err)
	}
	var sourceCount int64
	gdb.Model(&model.FilmSource{}).Where("id = ?", "src1").Count(&sourceCount)
	if sourceCount != 1 {
		t.Fatal("采集中不应删除 src1")
	}
	var lines int64
	gdb.Model(&model.FilmSourcePlaylist{}).Where("source_id = ?", "src1").Count(&lines)
	if lines != 1 {
		t.Fatalf("采集中不应清线路，lines=%d", lines)
	}
	gdb.Model(&model.FilmSource{}).Where("id = ?", "src2").Count(&sourceCount)
	if sourceCount != 1 {
		t.Fatal("src2 should stay")
	}
	if !progress.IsTaskRunning("src2") {
		t.Fatal("another running source should stay running")
	}
}

func TestCollectService_UpdateFilmSource_CleanOldData(t *testing.T) {
	gdb := setupCascadeCleanTestDB(t)

	// 创建采集源 src1
	gdb.Create(&model.FilmSource{Id: "src1", Name: "旧源", Uri: "http://old-src.com", State: true})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 10, FirstSourceId: "src1"},
		FilmIndexContent:  model.FilmIndexContent{Name: "旧源独占影片"},
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 11, FirstSourceId: "src2"},
		FilmIndexContent:  model.FilmIndexContent{Name: "其他站影片"},
	})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 10, SourceId: "src1", LineKind: "play", Content: "[]"})
	gdb.Create(&model.FilmSourcePlaylist{Mid: 11, SourceId: "src2", LineKind: "play", Content: "[]"})
	gdb.Create(&model.FilmSource{Id: "src2", Name: "其他站", Uri: "http://other-src.com", State: true})
	gdb.Create(&model.Category{Id: 8, Pid: 0, Name: "旧分类", StableKey: "display:root:旧分类", Show: true})
	gdb.Create(&model.Category{Id: 9, Pid: 0, Name: "共用分类", StableKey: "display:root:共用分类", Show: true})
	gdb.Create(&model.SourceCategory{SourceId: "src1", SourceTypeId: 1, RawName: "旧分类"})
	gdb.Create(&model.CategoryMapping{SourceId: "src1", SourceTypeId: 1, CategoryId: 8})
	gdb.Create(&model.CategoryMapping{SourceId: "src2", SourceTypeId: 1, CategoryId: 9})

	srv := new(CollectService)
	updateReq := model.FilmSource{
		Id:    "src1",
		Name:  "新源",
		Uri:   "http://new-src.com",
		State: true,
	}
	if err := srv.UpdateFilmSource(updateReq, "", false); err != nil {
		t.Fatalf("UpdateFilmSource failed: %v", err)
	}

	// 验证: 旧独占影片应该被清理，旧线路被清空
	var filmCount int64
	gdb.Model(&model.FilmIndex{}).Where("mid = ?", 10).Count(&filmCount)
	if filmCount != 0 {
		t.Fatalf("expected old film 10 to be cleaned, got count=%d", filmCount)
	}
	var otherFilms int64
	gdb.Model(&model.FilmIndex{}).Where("mid = ?", 11).Count(&otherFilms)
	if otherFilms != 1 {
		t.Fatalf("expected the other source film to stay, got %d", otherFilms)
	}

	// 验证: 站点 URI 已成功更新
	var updatedSrc model.FilmSource
	gdb.Where("id = ?", "src1").First(&updatedSrc)
	if updatedSrc.Uri != "http://new-src.com" {
		t.Fatalf("expected uri http://new-src.com, got %s", updatedSrc.Uri)
	}

	var src1Raw int64
	var src1Map int64
	gdb.Model(&model.SourceCategory{}).Where("source_id = ?", "src1").Count(&src1Raw)
	gdb.Model(&model.CategoryMapping{}).Where("source_id = ?", "src1").Count(&src1Map)
	if src1Raw != 0 || src1Map != 0 {
		t.Fatalf("expected source data cleared, raw=%d map=%d", src1Raw, src1Map)
	}
	var exclusive model.Category
	if err := gdb.First(&exclusive, 8).Error; err == nil {
		t.Fatalf("category only used by the changed source should be removed")
	}
	var shared model.Category
	if err := gdb.First(&shared, 9).Error; err != nil {
		t.Fatalf("category used by another source was deleted: %v", err)
	}
}

func TestCurrentPrimarySource_SkipsDeletingSource(t *testing.T) {
	gdb := setupCascadeCleanTestDB(t)
	gdb.Create(&model.FilmSource{Id: "src1", Name: "源1", Uri: "http://src1.com", State: true, Sort: 0})
	gdb.Create(&model.FilmSource{Id: "src2", Name: "源2", Uri: "http://src2.com", State: true, Sort: 1})
	gdb.Create(&model.FilmSource{Id: "src3", Name: "源3", Uri: "http://src3.com", State: true, Sort: 2})

	progress.SetSourceDeleting("src1", true)
	progress.SetSourceDeleting("src2", true)
	t.Cleanup(func() {
		progress.SetSourceDeleting("src1", false)
		progress.SetSourceDeleting("src2", false)
	})

	// 主站为空时懒初始化会跳过删除中的站
	primary := repobase.CurrentPrimarySource()
	if primary == nil || primary.Id != "src3" {
		t.Fatalf("expected primary to skip deleting sources and pick src3, got %#v", primary)
	}

	progress.SetSourceDeleting("src1", false)
	progress.SetSourceDeleting("src2", false)
	// 主站已持久化：排序首位恢复可用也不改变主站
	primary = repobase.CurrentPrimarySource()
	if primary == nil || primary.Id != "src3" {
		t.Fatalf("expected primary to stick to src3 after deleting flags cleared, got %#v", primary)
	}
}

func TestCollectService_TriggerDelFilmSourceBatch_SupportsPrimaryAndDeletesCleanly(t *testing.T) {
	gdb := setupCascadeCleanTestDB(t)
	gdb.Create(&model.FilmSource{Id: "src1", Name: "源1", Uri: "http://src1.com", State: true, Sort: 0})
	gdb.Create(&model.FilmSource{Id: "src2", Name: "源2", Uri: "http://src2.com", State: true, Sort: 1})
	gdb.Create(&model.FilmSource{Id: "src3", Name: "源3", Uri: "http://src3.com", State: true, Sort: 2})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, FirstSourceId: "src1"},
		FilmIndexContent:  model.FilmIndexContent{Name: "片1"},
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, FirstSourceId: "src2"},
		FilmIndexContent:  model.FilmIndexContent{Name: "片2"},
	})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 3, FirstSourceId: "src3"},
		FilmIndexContent:  model.FilmIndexContent{Name: "片3"},
	})
	if err := repobase.RefreshCollectSourceCatalog(); err != nil {
		t.Fatal(err)
	}

	srv := new(CollectService)
	// 批量删除包含当前主站 src1：自动交接至未删除的启用站 src3，src1 排在最后删除
	if err := srv.TriggerDelFilmSourceBatch([]string{"src1", "src2"}, "src3", false); err != nil {
		t.Fatalf("TriggerDelFilmSourceBatch failed: %v", err)
	}

	// 入队后主站已交接至 src3
	if curPrimary := repobase.CurrentPrimarySource(); curPrimary == nil || curPrimary.Id != "src3" {
		t.Fatalf("expected primary src3 while deleting, got %#v", curPrimary)
	}

	waitSearchWorkIdle(t)

	var leftSources int64
	gdb.Model(&model.FilmSource{}).Where("id IN ?", []string{"src1", "src2"}).Count(&leftSources)
	if leftSources != 0 {
		t.Fatalf("expected src1 and src2 deleted, remaining=%d", leftSources)
	}
	var leftFilms int64
	gdb.Model(&model.FilmIndex{}).Where("mid IN ?", []int64{1, 2}).Count(&leftFilms)
	if leftFilms != 0 {
		t.Fatalf("expected films 1 and 2 deleted, remaining=%d", leftFilms)
	}
	if curPrimary := repobase.CurrentPrimarySource(); curPrimary == nil || curPrimary.Id != "src3" {
		t.Fatalf("expected final primary src3, got %#v", curPrimary)
	}
}

func TestCollectService_RejectsUpdateAndStateChangeWhileDeleting(t *testing.T) {
	gdb := setupCascadeCleanTestDB(t)
	src := model.FilmSource{Id: "src_del_guard", Name: "删除中站", Uri: "http://del.example/api", State: true}
	if err := gdb.Create(&src).Error; err != nil {
		t.Fatal(err)
	}
	progress.SetSourceDeleting(src.Id, true)
	t.Cleanup(func() { progress.SetSourceDeleting(src.Id, false) })

	srv := new(CollectService)
	next := src
	next.Name = "改名"
	if err := srv.UpdateFilmSource(next, "", false); err == nil || err.Error() != "该站正在删除" {
		t.Fatalf("UpdateFilmSource err = %v, want 该站正在删除", err)
	}
	if err := srv.BatchUpdateFilmSourceState([]string{src.Id}, false); err == nil || err.Error() != "该站正在删除" {
		t.Fatalf("BatchUpdateFilmSourceState err = %v, want 该站正在删除", err)
	}
	var saved model.FilmSource
	if err := gdb.First(&saved, "id = ?", src.Id).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Name != src.Name || !saved.State {
		t.Fatalf("source should remain untouched while deleting, got %+v", saved)
	}
}

func TestCollectService_BatchUpdateFilmSourceState_RejectsBusySource(t *testing.T) {
	gdb := setupCascadeCleanTestDB(t)
	srcIdle := model.FilmSource{Id: "src_idle_batch", Name: "空闲站", Uri: "http://idle.example/api", State: true}
	srcBusy := model.FilmSource{Id: "src_busy_batch", Name: "采集站", Uri: "http://busy.example/api", State: true}
	if err := gdb.Create(&srcIdle).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&srcBusy).Error; err != nil {
		t.Fatal(err)
	}
	progress.MarkSourcesCollectStarting([]model.FilmSource{srcBusy}, "q-busy-state")
	if _, err := progress.TryRegisterTask(srcBusy.Id, "req-busy-state", nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		progress.UnregisterTask(srcBusy.Id, "req-busy-state")
		progress.MarkStopped(srcBusy.Id)
	})

	srv := new(CollectService)
	if err := srv.BatchUpdateFilmSourceState([]string{srcIdle.Id, srcBusy.Id}, false); err == nil {
		t.Fatal("expected busy batch state update to be rejected")
	}

	var idleSaved, busySaved model.FilmSource
	if err := gdb.First(&idleSaved, "id = ?", srcIdle.Id).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.First(&busySaved, "id = ?", srcBusy.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !idleSaved.State || !busySaved.State {
		t.Fatalf("expected atomic rejection leaving both enabled, idle=%v busy=%v", idleSaved.State, busySaved.State)
	}

	// 验证分类采集中同样拒绝批量操作
	progress.UnregisterTask(srcBusy.Id, "req-busy-state")
	progress.MarkStopped(srcBusy.Id)
	progress.BroadcastCategoryStatus(srcBusy.Id, "syncing")
	t.Cleanup(func() { progress.BroadcastCategoryStatus(srcBusy.Id, "idle") })
	if err := srv.BatchUpdateFilmSourceState([]string{srcBusy.Id}, false); err == nil {
		t.Fatal("expected category-syncing batch state update to be rejected")
	}
}

func TestCollectService_TriggerDelFilmSourceBatch_RejectsBusyAtomically(t *testing.T) {
	gdb := setupCascadeCleanTestDB(t)
	srcIdle := model.FilmSource{Id: "src_idle_del_b", Name: "空闲站", Uri: "http://idle-del.example/api", State: true}
	srcBusy := model.FilmSource{Id: "src_busy_del_b", Name: "采集中站", Uri: "http://busy-del.example/api", State: true}
	if err := gdb.Create(&srcIdle).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&srcBusy).Error; err != nil {
		t.Fatal(err)
	}
	if err := repobase.RefreshCollectSourceCatalog(); err != nil {
		t.Fatal(err)
	}
	progress.MarkSourcesCollectStarting([]model.FilmSource{srcBusy}, "q-busy-del")
	if _, err := progress.TryRegisterTask(srcBusy.Id, "req-busy-del", nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		progress.UnregisterTask(srcBusy.Id, "req-busy-del")
		progress.MarkStopped(srcBusy.Id)
	})

	srv := new(CollectService)
	if err := srv.TriggerDelFilmSourceBatch([]string{srcIdle.Id, srcBusy.Id}, "", false); err == nil {
		t.Fatal("expected batch delete to reject when a source is busy")
	}
	if progress.IsSourceDeleting(srcIdle.Id) {
		t.Fatal("rejected batch delete should not enqueue idle source")
	}
	var count int64
	gdb.Model(&model.FilmSource{}).Where("id = ?", srcIdle.Id).Count(&count)
	if count != 1 {
		t.Fatalf("expected idle source to remain untouched, count=%d", count)
	}
}

func TestCollectService_UpdateFilmSource_RejectsUriChangeAtomicallyWhenSearchBusy(t *testing.T) {
	gdb := setupCascadeCleanTestDB(t)
	src := model.FilmSource{Id: "src_uri_busy", Name: "原站", Uri: "http://old.example/api", State: true}
	if err := gdb.Create(&src).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 100, FirstSourceId: src.Id},
		FilmIndexContent:  model.FilmIndexContent{Name: "原站影片"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSourcePlaylist{Mid: 100, SourceId: src.Id, LineKind: "play", Content: "[]"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.CategoryMapping{SourceId: src.Id, SourceTypeId: 1, CategoryId: 8}).Error; err != nil {
		t.Fatal(err)
	}

	filmsnapshot.NotifySearchIndexProgress(1, 0, "indexing")
	t.Cleanup(func() { filmsnapshot.NotifySearchIndexProgress(0, 0, "idle") })

	srv := new(CollectService)
	next := src
	next.Uri = "http://new.example/api"
	err := srv.UpdateFilmSource(next, "", false)
	if err == nil || err.Error() != filmsnapshot.ErrSearchIndexBusy.Error() {
		t.Fatalf("UpdateFilmSource err = %v, want %v", err, filmsnapshot.ErrSearchIndexBusy)
	}

	var saved model.FilmSource
	if err := gdb.First(&saved, "id = ?", src.Id).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Uri != src.Uri {
		t.Fatalf("source uri changed despite rejection: got %s want %s", saved.Uri, src.Uri)
	}
	var filmCount, lineCount, mapCount int64
	gdb.Model(&model.FilmIndex{}).Where("first_source_id = ?", src.Id).Count(&filmCount)
	gdb.Model(&model.FilmSourcePlaylist{}).Where("source_id = ?", src.Id).Count(&lineCount)
	gdb.Model(&model.CategoryMapping{}).Where("source_id = ?", src.Id).Count(&mapCount)
	if filmCount != 1 || lineCount != 1 || mapCount != 1 {
		t.Fatalf("data should remain intact when UpdateFilmSource rejected, films=%d lines=%d maps=%d", filmCount, lineCount, mapCount)
	}
}

func TestCollectService_UpdateFilmSource_RejectsUriAndStateChangeWhenCategorySyncing(t *testing.T) {
	gdb := setupCascadeCleanTestDB(t)
	src := model.FilmSource{Id: "src_cat_sync_edit", Name: "同步中站", Uri: "http://old.example/api", State: true}
	if err := gdb.Create(&src).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 101, FirstSourceId: src.Id},
		FilmIndexContent:  model.FilmIndexContent{Name: "保留影片"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSourcePlaylist{Mid: 101, SourceId: src.Id, LineKind: "play", Content: "[]"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.CategoryMapping{SourceId: src.Id, SourceTypeId: 1, CategoryId: 8}).Error; err != nil {
		t.Fatal(err)
	}

	progress.BroadcastCategoryStatus(src.Id, "syncing")
	t.Cleanup(func() { progress.BroadcastCategoryStatus(src.Id, "idle") })

	srv := new(CollectService)
	next := src
	next.Uri = "http://new.example/api"
	next.State = false
	if err := srv.UpdateFilmSource(next, "", false); err == nil {
		t.Fatal("expected UpdateFilmSource to reject while category syncing")
	}

	var saved model.FilmSource
	if err := gdb.First(&saved, "id = ?", src.Id).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Uri != src.Uri || !saved.State {
		t.Fatalf("source changed despite rejection: got uri=%s state=%v", saved.Uri, saved.State)
	}
	var filmCount, lineCount, mapCount int64
	gdb.Model(&model.FilmIndex{}).Where("first_source_id = ?", src.Id).Count(&filmCount)
	gdb.Model(&model.FilmSourcePlaylist{}).Where("source_id = ?", src.Id).Count(&lineCount)
	gdb.Model(&model.CategoryMapping{}).Where("source_id = ?", src.Id).Count(&mapCount)
	if filmCount != 1 || lineCount != 1 || mapCount != 1 {
		t.Fatalf("data must not be wiped when UpdateFilmSource fails validation, films=%d lines=%d maps=%d", filmCount, lineCount, mapCount)
	}
}

func TestCollectService_UpdateFilmSource_RejectsDuplicateUriBeforeClean(t *testing.T) {
	gdb := setupCascadeCleanTestDB(t)
	src1 := model.FilmSource{Id: "src_dup_1", Name: "站1", Uri: "http://dup1.example/api", State: true, Sort: 0}
	src2 := model.FilmSource{Id: "src_dup_2", Name: "站2", Uri: "http://dup2.example/api", State: true, Sort: 1}
	if err := gdb.Create(&src1).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&src2).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 888, FirstSourceId: src1.Id},
		FilmIndexContent:  model.FilmIndexContent{Name: "站1宝贵影片"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSourcePlaylist{Mid: 888, SourceId: src1.Id, LineKind: "play", Content: "[]"}).Error; err != nil {
		t.Fatal(err)
	}

	srv := new(CollectService)
	next := src1
	next.Uri = src2.Uri
	if err := srv.UpdateFilmSource(next, "", false); err == nil {
		t.Fatal("expected duplicate URI error")
	}

	var filmCount, lineCount int64
	gdb.Model(&model.FilmIndex{}).Where("first_source_id = ?", src1.Id).Count(&filmCount)
	gdb.Model(&model.FilmSourcePlaylist{}).Where("source_id = ?", src1.Id).Count(&lineCount)
	if filmCount != 1 || lineCount != 1 {
		t.Fatalf("source data must NOT be cleaned when URI conflicts, films=%d lines=%d", filmCount, lineCount)
	}
}
