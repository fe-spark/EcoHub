package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/notify"
	"server/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupCollectServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := gdb.AutoMigrate(
		&model.FilmSource{},
		&model.SlaveMoviePlaylist{},
		&model.CollectSourceStats{},
		&model.Category{},
		&model.SourceCategory{},
		&model.CategoryMapping{},
		&model.FailureRecord{},
	); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	db.Mdb = gdb
	return gdb
}

func mockCollectServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"msg":"ok","page":1,"pagecount":1,"limit":"20","total":0,"list":[],"class":[{"type_id":1,"type_name":"电影"}]}`))
	}))
}

func TestCollectService_SaveFilmSource_MasterCleansSlavePlaylists(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)

	ts := mockCollectServer()
	defer ts.Close()

	// 先前已有主站
	gdb.Create(&model.FilmSource{
		Id:    "master_old",
		Name:  "旧主站",
		Uri:   ts.URL + "/old",
		Grade: model.MasterCollect,
		State: true,
	})

	// 新站（ID: src_new），但在附属表中存在历史残留
	gdb.Create(&model.SlaveMoviePlaylist{
		SourceId:   "src_new",
		MovieKey:   "k1",
		GroupIndex: 0,
		GroupName:  "线路1",
		Content:    "[]",
	})

	srv := &CollectService{}
	newMaster := model.FilmSource{
		Id:    "src_new",
		Name:  "新主站",
		Uri:   ts.URL + "/new",
		Grade: model.MasterCollect,
		State: true,
	}

	// 触发 SaveFilmSource
	err := srv.SaveFilmSource(newMaster)
	if err != nil {
		t.Fatalf("SaveFilmSource failed: %v", err)
	}

	// 1. 旧主站应被自动降级为附属站 (SlaveCollect)
	var oldMaster model.FilmSource
	if err := gdb.First(&oldMaster, "id = ?", "master_old").Error; err != nil {
		t.Fatalf("query old master: %v", err)
	}
	if oldMaster.Grade != model.SlaveCollect {
		t.Fatalf("expected old master to be demoted to SlaveCollect, got %v", oldMaster.Grade)
	}

	// 2. 新主站历史残留必须被物理清空
	var slaveCount int64
	gdb.Model(&model.SlaveMoviePlaylist{}).Where("source_id = ?", "src_new").Count(&slaveCount)
	if slaveCount != 0 {
		t.Fatalf("expected 0 residual slave playlists for new master, got %d", slaveCount)
	}
}

func TestCollectService_UpdateFilmSource_MasterDowngrade(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)

	// 原主站
	if err := gdb.Create(&model.FilmSource{
		Id:    "master_old",
		Name:  "旧主站",
		Uri:   "http://master.old/json",
		Grade: model.MasterCollect,
		State: true,
	}).Error; err != nil {
		t.Fatalf("create master_old failed: %v", err)
	}
	// 原附属站
	if err := gdb.Create(&model.FilmSource{
		Id:    "slave_1",
		Name:  "附属站1",
		Uri:   "http://slave1.old/json",
		Grade: model.SlaveCollect,
		State: true,
	}).Error; err != nil {
		t.Fatalf("create slave_1 failed: %v", err)
	}

	srv := &CollectService{}
	ts := mockCollectServer()
	defer ts.Close()

	// 试图将 master_old 直接降级为附属站，应当被拦截并拒绝
	demoted := model.FilmSource{
		Id:    "master_old",
		Name:  "旧主站降级",
		Uri:   "http://master.old/json",
		Grade: model.SlaveCollect,
		State: true,
	}

	err := srv.UpdateFilmSource(demoted)
	if err == nil {
		t.Fatal("expected error when downgrading master source directly, got nil")
	}
	if !strings.Contains(err.Error(), "系统必须保留一个主站，主站不可直接降级为附属站") {
		t.Fatalf("unexpected error message: %v", err)
	}

	var updated model.FilmSource
	if err := gdb.First(&updated, "id = ?", "master_old").Error; err != nil {
		t.Fatalf("query updated master_old: %v", err)
	}
	if updated.Grade != model.MasterCollect {
		t.Fatalf("expected master_old grade to remain MasterCollect, got %v", updated.Grade)
	}

	// 排空在途异步通知协程，确保测试结束前完全执行完毕
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer drainCancel()
	_ = notify.WaitPendingPublishes(drainCtx)
}

func TestCollectService_FailureRecords(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	srv := &CollectService{}

	// 1. 插入测试失败记录
	rec1 := model.FailureRecord{
		OriginId:   "src_1",
		OriginName: "源站1",
		Uri:        "http://src1/api",
		PageNumber: 1,
		Hour:       24,
		Cause:      "timeout",
		Status:     model.FailureRecordStatusPending,
		RetryCount: 1,
	}
	rec2 := model.FailureRecord{
		OriginId:   "src_1",
		OriginName: "源站1",
		Uri:        "http://src1/api",
		PageNumber: 2,
		Hour:       24,
		Cause:      "rate limit",
		Status:     model.FailureRecordStatusSuccess,
		RetryCount: 2,
	}
	rec3 := model.FailureRecord{
		OriginId:   "src_2",
		OriginName: "源站2",
		Uri:        "http://src2/api",
		PageNumber: 3,
		Hour:       24,
		Cause:      "server error",
		Status:     model.FailureRecordStatusFailed,
		RetryCount: 5,
	}
	gdb.Create(&rec1)
	gdb.Create(&rec2)
	gdb.Create(&rec3)

	// 2. 测试 GetRecordList
	params := model.RecordRequestVo{
		Paging: &dto.Page{Current: 1, PageSize: 10},
		Status: -1,
	}
	list := srv.GetRecordList(params)
	if len(list) != 3 {
		t.Fatalf("expected 3 failure records, got %d", len(list))
	}

	// 3. 测试 GetRecordOptions
	opts := srv.GetRecordOptions()
	if len(opts["status"]) == 0 {
		t.Fatalf("expected non-empty status options")
	}

	// 4. 测试 ClearRetriedRecords (应清理 success 和 failed，保留 pending)
	srv.ClearRetriedRecords()
	listAfterClear := srv.GetRecordList(params)
	if len(listAfterClear) != 1 {
		t.Fatalf("expected 1 record after ClearRetriedRecords, got %d", len(listAfterClear))
	}
	if listAfterClear[0].Status != model.FailureRecordStatusPending {
		t.Fatalf("expected remaining record to be pending, got %d", listAfterClear[0].Status)
	}

	// 5. 测试 ClearAllRecord (truncate table)
	srv.ClearAllRecord()
	listAfterTruncate := srv.GetRecordList(params)
	if len(listAfterTruncate) != 0 {
		t.Fatalf("expected 0 records after ClearAllRecord, got %d", len(listAfterTruncate))
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
		Grade:  model.SlaveCollect,
		Format: model.SourceFormatJSON,
	}
	gdb.Create(&s)

	// 添加一条该源的失败记录
	fr := model.FailureRecord{
		OriginId:   s.Id,
		OriginName: s.Name,
		PageNumber: 1,
		Status:     model.FailureRecordStatusPending,
	}
	gdb.Create(&fr)

	// 更新为 XML 格式
	sUpdate := s
	sUpdate.Format = model.SourceFormatXML
	labels := sourceChangeLabels(s, sUpdate)
	foundFormatLabel := false
	for _, l := range labels {
		if l == "接口格式: JSON → XML" {
			foundFormatLabel = true
			break
		}
	}
	if !foundFormatLabel {
		t.Fatalf("expected format change label '接口格式: JSON → XML', got: %v", labels)
	}

	if err := srv.UpdateFilmSource(sUpdate); err != nil {
		t.Fatalf("UpdateFilmSource failed: %v", err)
	}

	// 验证失败记录已被清空
	var remainingFrCount int64
	gdb.Model(&model.FailureRecord{}).Where("origin_id = ?", s.Id).Count(&remainingFrCount)
	if remainingFrCount != 0 {
		t.Fatalf("expected failure records to be cleaned after format change, got %d", remainingFrCount)
	}
}

func TestCollectService_AddFilmSource_OrderAtTheEnd(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	srv := &CollectService{}

	baseTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	// 插入一个主站和两个附属站
	master := model.FilmSource{
		Id:        "master_site",
		Name:      "主站",
		Uri:       "https://master.example.com/api",
		Grade:     model.MasterCollect,
		State:     true,
		CreatedAt: baseTime,
	}
	slave1 := model.FilmSource{
		Id:        "slave_1_zz", // 故意使用字典序较大的 id
		Name:      "附属站1",
		Uri:       "https://slave1.example.com/api",
		Grade:     model.SlaveCollect,
		State:     true,
		CreatedAt: baseTime.Add(1 * time.Minute),
	}
	slave2 := model.FilmSource{
		Id:        "slave_2_mm",
		Name:      "附属站2",
		Uri:       "https://slave2.example.com/api",
		Grade:     model.SlaveCollect,
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

	// 新增一个附属站（id 字典序可能极小，如 "000_new_slave"）
	newSlave := model.FilmSource{
		Id:        "000_new_slave",
		Name:      "新增附属站",
		Uri:       "https://newslave.example.com/api",
		Grade:     model.SlaveCollect,
		State:     true,
		CreatedAt: baseTime.Add(3 * time.Minute),
	}
	if err := srv.SaveFilmSource(newSlave); err != nil {
		t.Fatalf("SaveFilmSource failed: %v", err)
	}

	list := srv.GetFilmSourceList()
	if len(list) != 4 {
		t.Fatalf("expected 4 sources, got %d", len(list))
	}

	// 验证顺序：主站第一，附属站按创建时间排序，新增采集站排在最后一个
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

	listAfterUpdate := srv.GetFilmSourceList()
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
		{Id: "zzz_source", Name: "站点Z", Uri: "https://z.com/api", Grade: model.SlaveCollect, State: true},
		{Id: "mmm_source", Name: "站点M", Uri: "https://m.com/api", Grade: model.SlaveCollect, State: true},
		{Id: "aaa_source", Name: "站点A", Uri: "https://a.com/api", Grade: model.SlaveCollect, State: true},
	}

	if err := repository.ReplaceCollectSources(sources); err != nil {
		t.Fatalf("ReplaceCollectSources failed: %v", err)
	}

	list := repository.GetCollectSourceList()
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



