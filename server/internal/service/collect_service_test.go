package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider/progress"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupCollectServiceTestDB(t *testing.T) *gorm.DB {
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
		&model.FilmSourcePlaylist{},
		&model.CollectSourceStats{},
		&model.Category{},
		&model.SourceCategory{},
		&model.CategoryMapping{},
		&model.ProxyConfigRecord{},
		&model.NotifyConfigRecord{},
		&model.BannerSlot{},
		&model.SiteConfigRecord{},
	); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	// 上一个用例触发的检索重建/预热在后台跑（防抖 500ms），没结束时改站点会被拒绝；先等它们空闲。
	waitSearchWorkIdle(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() {
		waitSearchWorkIdle(t)
		db.Mdb = prev
	})
	return gdb
}

// waitSearchWorkIdle 等后台的检索重建（含防抖定时器）和主站预热结束，避免用例之间互相影响。
func waitSearchWorkIdle(t *testing.T) {
	t.Helper()
	// 先等切主站的预热协程（它会先睡 150ms 才置 isPrimaryWarming，光看标志会漏掉）。
	done := make(chan struct{})
	go func() { primaryWarmups.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("primary warmup did not finish")
	}
	deadline := time.Now().Add(15 * time.Second)
	for IsPrimarySiteSwitching() || filmsnapshot.SearchRebuildScheduled() {
		if time.Now().After(deadline) {
			t.Fatal("search index rebuild / primary warmup did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	filmsnapshot.WaitActiveFilmSearchIndexBuilt()
}

func mockCollectServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"msg":"ok","page":1,"pagecount":1,"limit":"20","total":0,"list":[],"class":[{"type_id":1,"type_name":"电影"}]}`))
	}))
}

func TestGetFilmSourceList_MarksMissingCategory(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "ready", Name: "有分类", Uri: "http://ready", State: true, Sort: 0}).Error; err != nil {
		t.Fatalf("create ready: %v", err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "missing", Name: "缺分类", Uri: "http://missing", State: true, Sort: 1}).Error; err != nil {
		t.Fatalf("create missing: %v", err)
	}
	if err := gdb.Create(&model.CategoryMapping{SourceId: "ready", SourceTypeId: 1, CategoryId: 9}).Error; err != nil {
		t.Fatalf("create mapping: %v", err)
	}

	list, err := (&CollectService{}).GetFilmSourceList()
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]model.FilmSourceListItem, len(list))
	for _, item := range list {
		byID[item.Id] = item
	}
	if !byID["ready"].CategoryReady {
		t.Fatal("source with category mapping should be collectable")
	}
	if byID["missing"].CategoryReady {
		t.Fatal("source without category should be marked")
	}
}

func TestCollectService_SaveFilmSource_SortOrdering(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)

	ts := mockCollectServer()
	defer ts.Close()

	gdb.Create(&model.FilmSource{
		Id:    "src_1",
		Name:  "站1",
		Uri:   ts.URL + "/1",
		Sort:  0,
		State: true,
	})

	srv := &CollectService{}
	newSrc := model.FilmSource{
		Id:    "src_2",
		Name:  "站2",
		Uri:   ts.URL + "/2",
		State: true,
	}

	err := srv.SaveFilmSource(newSrc)
	if err != nil {
		t.Fatalf("SaveFilmSource failed: %v", err)
	}

	list, err := srv.GetFilmSourceList()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 2 {
		t.Fatalf("expected at least 2 sources, got %d", len(list))
	}
	if list[0].Id != "src_1" {
		t.Fatalf("expected src_1 to be first, got %s", list[0].Id)
	}
	if list[1].Id != "src_2" {
		t.Fatalf("expected src_2 (added at end) to be second, got %s", list[1].Id)
	}

	// 测试拖拽排序
	if err := srv.SortFilmSources([]string{"src_2", "src_1"}); err != nil {
		t.Fatalf("SortFilmSources failed: %v", err)
	}
	sortedList, err := srv.GetFilmSourceList()
	if err != nil {
		t.Fatal(err)
	}
	if sortedList[0].Id != "src_2" || sortedList[1].Id != "src_1" {
		t.Fatalf("expected src_2 to be first after sorting, got %+v", sortedList)
	}
	var categoryCount int64
	gdb.Model(&model.CategoryMapping{}).Count(&categoryCount)
	if categoryCount != 0 {
		t.Fatalf("switching preferred source must not fetch categories, got %d mappings", categoryCount)
	}
}

func TestSortFilmSources_AllowsPrimaryChangeWhileCollecting(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "src_1", Name: "站1", Uri: "http://1", Sort: 0, State: true}).Error; err != nil {
		t.Fatalf("create src_1: %v", err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "src_2", Name: "站2", Uri: "http://2", Sort: 1, State: true}).Error; err != nil {
		t.Fatalf("create src_2: %v", err)
	}
	progress.MarkSourcesCollectStarting([]model.FilmSource{{Id: "src_1", Name: "站1"}}, "q-test")
	// 真实的采集在跑时会登记任务；只有进度、没有任务也没有占用的排队会被当成残留当场清掉，
	// 所以这里要登记一个任务，才算"正在采集"。
	if _, err := progress.TryRegisterTask("src_1", "req-test", nil); err != nil {
		t.Fatalf("register running task: %v", err)
	}
	t.Cleanup(func() {
		progress.UnregisterTask("src_1", "req-test")
		progress.MarkStopped("src_1")
	})

	srv := &CollectService{}
	if err := srv.SortFilmSources([]string{"src_2", "src_1"}); err != nil {
		t.Fatalf("switch while collecting: %v", err)
	}
	list, err := srv.GetFilmSourceList()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 || list[0].Id != "src_2" || !list[0].IsPrimary {
		t.Fatalf("primary should be src_2, got %+v", list)
	}
	if !progress.IsTaskRunning("src_1") {
		t.Fatal("the running collect should stay running")
	}
	if err := srv.SortFilmSources([]string{"src_2", "src_1"}); err != nil {
		t.Fatalf("reordering without changing primary should pass: %v", err)
	}
	if err := srv.UpdateFilmSource(model.FilmSource{Id: "src_2", Name: "站2", Uri: "http://2", State: false}); err != nil {
		t.Fatalf("disabling primary while collecting should pass: %v", err)
	}
	list, err = srv.GetFilmSourceList()
	if err != nil {
		t.Fatal(err)
	}
	var primaryID string
	for _, item := range list {
		if item.IsPrimary {
			primaryID = item.Id
		}
	}
	if primaryID != "src_1" {
		t.Fatalf("primary should fall back to src_1, got %+v", list)
	}
}

func TestSyncSourceCategories_MissingSourceClearsSyncing(t *testing.T) {
	setupCollectServiceTestDB(t)
	progress.BroadcastCategoryStatus("gone-src", "syncing")
	t.Cleanup(func() { progress.BroadcastCategoryStatus("gone-src", "idle") })

	_, err := SpiderSvc.SyncSourceCategories("gone-src", "")
	if err == nil || err.Error() != "采集站信息不存在" {
		t.Fatalf("err=%v", err)
	}
	if progress.IsCategorySyncing("gone-src") {
		t.Fatal("missing source left category syncing")
	}
}

func TestSyncSourceCategories_StoresCopyForThatSource(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	ts := mockCollectServer()
	defer ts.Close()

	if err := gdb.Create(&model.FilmSource{Id: "src_sync", Name: "同步站", Uri: ts.URL, State: true}).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	if _, err := SpiderSvc.SyncSourceCategories("src_sync", ""); err != nil {
		t.Fatalf("SyncSourceCategories: %v", err)
	}

	var rawCount int64
	var mapCount int64
	gdb.Model(&model.SourceCategory{}).Where("source_id = ?", "src_sync").Count(&rawCount)
	gdb.Model(&model.CategoryMapping{}).Where("source_id = ?", "src_sync").Count(&mapCount)
	if rawCount == 0 || mapCount == 0 {
		t.Fatalf("expected a stored category copy, raw=%d map=%d", rawCount, mapCount)
	}
}

func TestCollectService_UpdateFilmSource_NameChange(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)

	s := model.FilmSource{
		Id:    "src_name_test",
		Name:  "测试源",
		Uri:   "https://example.com/api",
		State: true,
		Sort:  0,
	}
	gdb.Create(&s)

	srv := &CollectService{}
	sUpdate := s
	sUpdate.Name = "新名称"
	if err := srv.UpdateFilmSource(sUpdate); err != nil {
		t.Fatalf("UpdateFilmSource failed: %v", err)
	}

	var updated model.FilmSource
	if err := gdb.First(&updated, "id = ?", "src_name_test").Error; err != nil {
		t.Fatalf("query updated: %v", err)
	}
	if updated.Name != "新名称" {
		t.Fatalf("expected name 新名称, got %s", updated.Name)
	}
}

func TestCollectService_UpdateFilmSource_FormatChange(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	srv := &CollectService{}

	s := model.FilmSource{
		Id:     "src_format_test",
		Name:   "格式测试源",
		Uri:    "https://example.com/api",
		State:  true,
		Sort:   0,
		Format: model.SourceFormatJSON,
	}
	gdb.Create(&s)

	sUpdate := s
	sUpdate.Format = model.SourceFormatXML
	if err := srv.UpdateFilmSource(sUpdate); err != nil {
		t.Fatalf("UpdateFilmSource failed: %v", err)
	}
	var updated model.FilmSource
	if err := gdb.First(&updated, "id = ?", s.Id).Error; err != nil {
		t.Fatalf("query updated: %v", err)
	}
	if updated.Format != model.SourceFormatXML {
		t.Fatalf("expected xml format, got %s", updated.Format)
	}
}

func TestCollectService_AddFilmSource_OrderAtTheEnd(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	srv := &CollectService{}

	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	// 插入三个按顺序的站
	master := model.FilmSource{
		Id:        "master_site",
		Name:      "主站",
		Uri:       "https://master.example.com/api",
		Sort:      0,
		State:     true,
		CreatedAt: baseTime,
	}
	slave1 := model.FilmSource{
		Id:        "slave_1_zz", // 故意使用字典序较大的 id
		Name:      "附属站1",
		Uri:       "https://slave1.example.com/api",
		Sort:      1,
		State:     true,
		CreatedAt: baseTime.Add(1 * time.Minute),
	}
	slave2 := model.FilmSource{
		Id:        "slave_2_mm",
		Name:      "附属站2",
		Uri:       "https://slave2.example.com/api",
		Sort:      2,
		State:     true,
		CreatedAt: baseTime.Add(2 * time.Minute),
	}
	if err := gdb.Create(&master).Error; err != nil {
		t.Fatalf("create master: %v", err)
	}
	if err := gdb.Create(&slave1).Error; err != nil {
		t.Fatalf("create slave1: %v", err)
	}
	if err := gdb.Create(&slave2).Error; err != nil {
		t.Fatalf("create slave2: %v", err)
	}

	// 新增一个站点（id 字典序可能极小，如 "000_new_slave"）
	newSlave := model.FilmSource{
		Id:        "000_new_slave",
		Name:      "新增附属站",
		Uri:       "https://newslave.example.com/api",
		State:     true,
		CreatedAt: baseTime.Add(3 * time.Minute),
	}
	if err := srv.SaveFilmSource(newSlave); err != nil {
		t.Fatalf("SaveFilmSource failed: %v", err)
	}

	list, err := srv.GetFilmSourceList()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 {
		t.Fatalf("expected 4 sources, got %d", len(list))
	}

	// 验证顺序：高权重第一，同权重按创建时间排序，新增采集站排在最后一个
	if list[0].Id != "master_site" {
		t.Errorf("expected list[0] to be master_site, got %s", list[0].Id)
	}
	if list[1].Id != "slave_1_zz" {
		t.Errorf("expected list[1] to be slave_1_zz, got %s", list[1].Id)
	}
	if list[2].Id != "slave_2_mm" {
		t.Errorf("expected list[2] to be slave_2_mm, got %s", list[2].Id)
	}
	if list[3].Id != "000_new_slave" {
		t.Errorf("expected list[3] (last item) to be 000_new_slave, got %s", list[3].Id)
	}

	// 更新 slave1 配置，验证其位置没有发生变化且 CreatedAt 保持不变
	updateSlave1 := slave1
	updateSlave1.Name = "附属站1重命名"
	updateSlave1.CreatedAt = time.Time{} // 前端提交通常无 CreatedAt
	if err := srv.UpdateFilmSource(updateSlave1); err != nil {
		t.Fatalf("UpdateFilmSource failed: %v", err)
	}

	listAfterUpdate, err := srv.GetFilmSourceList()
	if err != nil {
		t.Fatal(err)
	}
	if listAfterUpdate[1].Id != "slave_1_zz" || listAfterUpdate[1].Name != "附属站1重命名" {
		t.Errorf("expected list[1] to remain slave_1_zz after update, got %+v", listAfterUpdate[1])
	}
	if listAfterUpdate[3].Id != "000_new_slave" {
		t.Errorf("expected list[3] to remain 000_new_slave, got %s", listAfterUpdate[3].Id)
	}
	if listAfterUpdate[1].CreatedAt.Unix() != slave1.CreatedAt.Unix() {
		t.Errorf("expected slave1 CreatedAt to be preserved (%v), got %v", slave1.CreatedAt, listAfterUpdate[1].CreatedAt)
	}
}

func TestRepository_ReplaceCollectSources_PreservesOrderWithoutCreatedAt(t *testing.T) {
	setupCollectServiceTestDB(t)

	// 模拟从不含 createdAt 的旧备份恢复，id 使用逆序字典序测试是否严格按列表顺序保存
	sources := []model.FilmSource{
		{Id: "zzz_source", Name: "站点Z", Uri: "https://z.com/api", Sort: 0, State: true},
		{Id: "mmm_source", Name: "站点M", Uri: "https://m.com/api", Sort: 1, State: true},
		{Id: "aaa_source", Name: "站点A", Uri: "https://a.com/api", Sort: 2, State: true},
	}

	if err := repository.ReplaceCollectSources(sources); err != nil {
		t.Fatalf("ReplaceCollectSources failed: %v", err)
	}

	list, err := repository.GetCollectSourceList()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 sources, got %d", len(list))
	}

	// 验证列表严格按输入顺序恢复，不会退化回 id ASC 字典序
	expectedIDs := []string{"zzz_source", "mmm_source", "aaa_source"}
	for i, expectedID := range expectedIDs {
		if list[i].Id != expectedID {
			t.Errorf("expected list[%d].Id == %s, got %s", i, expectedID, list[i].Id)
		}
		if i > 0 && !list[i].CreatedAt.After(list[i-1].CreatedAt) {
			t.Errorf("expected list[%d] CreatedAt (%v) to be strictly after list[%d] CreatedAt (%v)", i, list[i].CreatedAt, i-1, list[i-1].CreatedAt)
		}
	}
}

func markSourceSealing(t *testing.T, id, name string) {
	t.Helper()
	progress.Ensure(id, name)
	progress.Update(id, func(p *model.CollectProgress) {
		p.Status = progress.StatusSealing
	})
	t.Cleanup(func() { progress.MarkSealed([]string{id}) })
}

func TestUpdateFilmSource_RejectsAddressChangeWhileSealing(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	src := model.FilmSource{Id: "seal_edit", Name: "待检索站", Uri: "https://old.example/api", State: true}
	if err := gdb.Create(&src).Error; err != nil {
		t.Fatal(err)
	}
	markSourceSealing(t, src.Id, src.Name)
	if !progress.IsTaskRunning(src.Id) {
		t.Fatal("待检索应算采集中")
	}

	next := src
	next.Uri = "https://new.example/api"
	err := (&CollectService{}).UpdateFilmSource(next)
	if err == nil || !strings.Contains(err.Error(), "待检索") {
		t.Fatalf("err = %v", err)
	}
	var saved model.FilmSource
	if err := gdb.First(&saved, "id = ?", src.Id).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Uri != src.Uri {
		t.Fatalf("uri = %s", saved.Uri)
	}

	next = src
	next.Name = "新名称"
	if err := (&CollectService{}).UpdateFilmSource(next); err != nil {
		t.Fatalf("改名应放行: %v", err)
	}
}

func TestTriggerDelFilmSource_RejectsWhileCollecting(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	src := model.FilmSource{Id: "seal_del", Name: "待检索站", Uri: "https://seal.example/api", State: true}
	if err := gdb.Create(&src).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.RefreshCollectSourceCatalog(); err != nil {
		t.Fatal(err)
	}
	markSourceSealing(t, src.Id, src.Name)

	err := (&CollectService{}).TriggerDelFilmSource(src.Id)
	if err == nil || err.Error() != "采集中，暂不可删除" {
		t.Fatalf("err = %v", err)
	}
	var n int64
	if err := gdb.Model(&model.FilmSource{}).Where("id = ?", src.Id).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("采集中不应删除，剩余 %d", n)
	}
	if progress.IsSourceDeleting(src.Id) {
		t.Fatal("拒绝后不应进入删除中")
	}
}
