package spider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider/fetcher"
	"server/internal/spider/progress"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupAuditSpiderDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(model.AllModels...); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	prevDB := db.Mdb
	db.Mdb = gdb
	repository.ResetCollectStatsCoalescer()
	t.Cleanup(func() {
		repository.ResetCollectStatsCoalescer()
		db.Mdb = prevDB
	})
	return gdb
}

// Fix 1：探测返回 0 页时也应完成生命周期收尾并更新定时补窗锚点与耗时统计，且同一站点不重复计算时长。
func TestZeroPageProbeStillCompletesAndUpdatesCronAnchor(t *testing.T) {
	resetSearchReseal()
	resetStreamPublishBufferForTest()
	origProbe := fetchProbePageCount
	origCollectPages := collectPagesFn
	origPub := publishStreamWindowFn
	origPref := preferredSourceIDFn
	origSeal := sealSearchIndex
	t.Cleanup(func() {
		fetchProbePageCount = origProbe
		collectPagesFn = origCollectPages
		publishStreamWindowFn = origPub
		preferredSourceIDFn = origPref
		sealSearchIndex = origSeal
		resetStreamPublishBufferForTest()
		resetSearchReseal()
	})
	publishStreamWindowFn = func([]int64) error { return nil }
	preferredSourceIDFn = func() string { return "" }
	sealSearchIndex = func([]int64) bool { return true }

	gdb := setupAuditSpiderDB(t)
	src := model.FilmSource{
		Id:     "zero-page-cron",
		Name:   "零页定时站",
		Uri:    "http://127.0.0.1:1/api",
		State:  true,
		Format: "json",
	}
	if err := gdb.Create(&src).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.Category{Id: 10, Name: "电影"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.CategoryMapping{SourceId: src.Id, SourceTypeId: 1, CategoryId: 10}).Error; err != nil {
		t.Fatal(err)
	}
	// 先写入 36 小时前的成功锚点，验证 resolveCollectHours 在探页阶段只算一次并沿用到采集阶段。
	past := time.Now().Add(-36 * time.Hour)
	if err := gdb.Create(&model.CollectSourceStats{
		SourceId:        src.Id,
		LastCollectTime: &past,
	}).Error; err != nil {
		t.Fatal(err)
	}

	var probedHours []int
	var collectCalled atomic.Bool
	fetchProbePageCount = func(_ context.Context, _ *model.FilmSource, hours int) (int, error) {
		probedHours = append(probedHours, hours)
		return 0, nil
	}
	collectPagesFn = func(context.Context, int, []int, *model.FilmSource, int, fetcher.Batch) (bool, int, int, []int, error) {
		collectCalled.Store(true)
		return false, 0, 0, nil, nil
	}

	beginCollectQueue()
	defer endCollectQueue()

	claim, err := claimCollectSources([]model.FilmSource{src}, model.CollectTagCronCollect)
	if err != nil || len(claim.Sources) != 1 {
		t.Fatalf("claim failed: %v", err)
	}
	batch := newCollectBatchContext(model.CollectTriggerCron, model.CollectTagCronCollect, claim.Sources, time.Now()).withClaim(claim)
	defer batch.close()

	runSourcesGroupWithLimit(claim.Sources, 24, model.CollectTagCronCollect, 1, stopAllVersion.Load(), batch)
	if collectCalled.Load() {
		t.Fatal("0 页站点不应真正调用 collectPagesFn")
	}
	if len(probedHours) != 1 || probedHours[0] < 36 {
		t.Fatalf("探页应携带补窗计算后的时长 >=36，实际=%v", probedHours)
	}
	if cached, ok := batch.resolvedSourceHour(src.Id); !ok || cached != probedHours[0] {
		t.Fatalf("batchCtx 应缓存探页阶段计算出的时长 %d, got %d (%v)", probedHours[0], cached, ok)
	}
	if err := batch.flushAndFinalize(); err != nil {
		t.Fatalf("flushAndFinalize failed: %v", err)
	}
	batch.emitSummary()

	last, err := repository.GetLastCollectTime(src.Id)
	if err != nil || last == nil || !last.After(past) {
		t.Fatalf("0 页定时采集也应推进 last_collect_time，past=%v got=%v err=%v", past, last, err)
	}
	stats := repository.GetCollectSourceRunStats([]string{src.Id})
	if st, ok := stats[src.Id]; !ok || st.LastRunTime == nil {
		t.Fatalf("0 页采集也应更新 last_run_time，actual=%+v", st)
	}
}

// Fix 3：cron 批次应按实际 cronTaskID 记录 PreV 起始时间，而非硬编码 sys_cron_auto_collect。
func TestCronBatchNoteStartedUsesActualCronTaskID(t *testing.T) {
	const customID = "task_custom_pre_v_42"
	taskRanAt.Delete(customID)
	taskRanAt.Delete("sys_cron_auto_collect")
	t.Cleanup(func() {
		taskRanAt.Delete(customID)
		taskRanAt.Delete("sys_cron_auto_collect")
	})

	claim := CollectClaim{
		Sources:    []model.FilmSource{{Id: "s1", Name: "S1"}},
		cronTaskID: customID,
	}
	batch := newCollectBatchContext(model.CollectTriggerCron, model.CollectTagCronCollect, claim.Sources, time.Now()).
		withClaim(claim)
	defer batch.close()

	batch.noteCronCollectStarted()
	if TaskPrev(customID).IsZero() {
		t.Fatalf("TaskPrev(%q) 应已记录实际启动时间", customID)
	}
	if !TaskPrev("sys_cron_auto_collect").IsZero() {
		t.Fatal("不应错误写入默认 sys_cron_auto_collect")
	}
}

// Fix 4：CollectFilms 连续更新多部影片时，第 1 部进入异步检索封口不能导致第 2 部被跳过。
func TestCollectFilms_MultiMidDoesNotSkipSubsequentWhenSealing(t *testing.T) {
	resetSearchReseal()
	resetStreamPublishBufferForTest()
	origPub := publishStreamWindowFn
	origPref := preferredSourceIDFn
	origSeal := sealSearchIndex
	origWait := waitCollectIdle
	t.Cleanup(func() {
		publishStreamWindowFn = origPub
		preferredSourceIDFn = origPref
		sealSearchIndex = origSeal
		waitCollectIdle = origWait
		resetStreamPublishBufferForTest()
		resetSearchReseal()
	})
	publishStreamWindowFn = func([]int64) error { return nil }
	preferredSourceIDFn = func() string { return "multi-film-src" }
	waitCollectIdle = func() {}
	sealSearchIndex = func([]int64) bool { return true }

	var seenIDs []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids := r.URL.Query().Get("ids")
		mu.Lock()
		seenIDs = append(seenIDs, ids)
		mu.Unlock()
		if ids == "2002" {
			// 验证进入第 2 部时，即使第 1 部已触发 searchReseal.running = true，外层队列仍保护第 2 部不被拦掉且只占 1 个名额。
			if reason := collectPauseReason(); reason != "" {
				t.Errorf("第 2 部影片期间不应因封口被拦截: %s", reason)
			}
			collectQueues.mu.Lock()
			qCount := collectQueues.n
			collectQueues.mu.Unlock()
			if qCount != 1 {
				t.Errorf("批量单片采集期间队列计数应为 1，实际=%d", qCount)
			}
		}
		resp := map[string]any{
			"code": 1,
			"list": []map[string]any{
				{
					"vod_id":        9001,
					"vod_name":      "测试片-" + ids,
					"type_id":       1,
					"vod_play_from": "dy",
					"vod_play_url":  "第1集$https://example.com/1.m3u8",
					"vod_time":      time.Now().Format("2006-01-02 15:04:05"),
				},
			},
		}
		if ids == "2002" {
			resp["list"].([]map[string]any)[0]["vod_id"] = 9002
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	gdb := setupAuditSpiderDB(t)
	src := model.FilmSource{Id: "multi-film-src", Name: "多片源", Uri: srv.URL, State: true, Format: "json"}
	if err := gdb.Create(&src).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.Category{Id: 10, Name: "电影"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.CategoryMapping{SourceId: src.Id, SourceTypeId: 1, CategoryId: 10}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create([]model.MovieSourceMapping{
		{GlobalMid: 101, SourceId: src.Id, SourceMid: 2001},
		{GlobalMid: 102, SourceId: src.Id, SourceMid: 2002},
	}).Error; err != nil {
		t.Fatal(err)
	}

	// 验证所有影片完成后外层释放队列才合并封口。
	sealDone := make(chan []int64, 4)
	sealSearchIndex = func(mids []int64) bool {
		sealDone <- append([]int64(nil), mids...)
		return true
	}

	CollectFilms([]int64{101, 102})

	select {
	case sealed := <-sealDone:
		if len(sealed) != 2 {
			t.Fatalf("应合并封口两部影片的 mid，实际=%v", sealed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("CollectFilms 结束后未能触发检索封口")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seenIDs) != 2 || seenIDs[0] != "2001" || seenIDs[1] != "2002" {
		t.Fatalf("应依次拉取全部两部影片 [2001 2002]，实际=%v", seenIDs)
	}
}

// Fix 5：搜索索引重建返回 idle 且连续失败达到重试上限后，自动释放 StatusSealing，不再永久卡死。
func TestSearchResealIdleRetryReleasesSealingAfterMaxRetries(t *testing.T) {
	resetSearchReseal()
	prevWait := sealRetryWait
	sealRetryWait = 5 * time.Millisecond
	t.Cleanup(func() {
		sealRetryWait = prevWait
		resetSearchReseal()
	})

	progress.Ensure("idle-max-src", "IdleMax")
	progress.Update("idle-max-src", func(p *model.CollectProgress) {
		p.Status = progress.StatusSealing
	})
	searchReseal.mu.Lock()
	searchReseal.sealing["idle-max-src"] = struct{}{}
	searchReseal.mu.Unlock()

	// 前 maxRebuildSealingRetries 次 idle 都处于awaitDone后触发重试计数与定时器；第 +1 次达到上限触发 complete。
	for i := 0; i < maxRebuildSealingRetries; i++ {
		searchReseal.mu.Lock()
		searchReseal.awaitDone = true
		searchReseal.mu.Unlock()
		searchReseal.onSearchIndexTerminal("idle")
		if snap, _ := progress.Snapshot("idle-max-src"); snap.Status != progress.StatusSealing {
			t.Fatalf("第 %d 次重试前不应提前释放 sealing，got %q", i+1, snap.Status)
		}
	}

	searchReseal.mu.Lock()
	searchReseal.awaitDone = true
	searchReseal.mu.Unlock()
	searchReseal.onSearchIndexTerminal("idle")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if snap, ok := progress.Snapshot("idle-max-src"); ok && snap.Status == progress.StatusFailed {
			if snap.Sealed {
				t.Fatalf("异常释放时 Sealed 必须为 false，got %+v", snap)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	snap, _ := progress.Snapshot("idle-max-src")
	t.Fatalf("达到最大重试上限后应释放为 failed，actual=%q", snap.Status)
}

// Fix 6：前置停用/失败站即使后续进度缓存被新一代占用覆盖，本批快照仍保留真实失败/停止状态。
func TestEarlyStoppedSourceSnapshotSurvivesOverwrite(t *testing.T) {
	resetSearchReseal()
	resetStreamPublishBufferForTest()
	t.Cleanup(func() {
		resetStreamPublishBufferForTest()
		resetSearchReseal()
	})
	gdb := setupAuditSpiderDB(t)

	s1 := model.FilmSource{Id: "early-stop-s1", Name: "早停站"}
	claim, err := claimCollectSources([]model.FilmSource{s1}, model.CollectTagBatchCollect)
	if err != nil || len(claim.Sources) != 1 {
		t.Fatalf("claim failed: %v", err)
	}
	batch := newCollectBatchContext(model.CollectTriggerManual, model.CollectTagBatchCollect, claim.Sources, time.Now()).withClaim(claim)
	defer batch.close()
	if err := batch.recordStart(); err != nil {
		t.Fatal(err)
	}

	// 模拟 dispatch 前已被一键终止，触发 runSourcesGroupWithLimit 的早停分支记录快照。
	oldVer := stopAllVersion.Load()
	StopAllCollect("提前终止")
	runSourcesGroupWithLimit(claim.Sources, 24, model.CollectTagBatchCollect, 1, oldVer, batch)

	// 立刻由新批次覆盖 s1 的 progress 状态（代次变更），验证旧批汇总仍能从 sourceSnapshots 保留 StatusStopped。
	claim2, err := claimCollectSources([]model.FilmSource{s1}, model.CollectTagSingleCollect)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseCollectSources(claim2.Sources)

	batch.emitSummary()
	list, _, err := repository.GetCollectRecordList(model.CollectRecordQuery{Page: 1, PageSize: 10})
	if err != nil || len(list) == 0 {
		t.Fatalf("query CollectRecord failed: %v len=%d", err, len(list))
	}
	var details []model.CollectRecordSourceDetail
	if err := json.Unmarshal([]byte(list[0].DetailJson), &details); err != nil {
		t.Fatalf("unmarshal DetailJson: %v", err)
	}
	if len(details) != 1 || details[0].Status != progress.StatusStopped {
		t.Fatalf("旧批次应保留 stopped 快照，actual=%+v", details)
	}
	_ = gdb
}

// Fix 7：runMaintenance 必须等 SearchWorkBusyReason() 清空后才执行维护回调。
func TestCollectLifecycle_RunMaintenanceWaitsForSearchWork(t *testing.T) {
	resetSearchReseal()
	t.Cleanup(resetSearchReseal)

	searchReseal.mu.Lock()
	searchReseal.running = true
	searchReseal.mu.Unlock()
	searchSealRunning.Store(true)

	ran := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- collectLifecycle.runMaintenance("测试等待检索完成", func() error {
			close(ran)
			return nil
		})
	}()

	time.Sleep(40 * time.Millisecond)
	select {
	case <-ran:
		t.Fatal("检索仍在封口时不应执行维护回调")
	default:
	}

	// 模拟正在重建时也应继续等待，直到全部检索忙碌标志清除。
	filmsnapshot.NotifySearchIndexProgress(10, 0, "indexing")
	searchReseal.mu.Lock()
	searchReseal.running = false
	searchReseal.mu.Unlock()
	searchSealRunning.Store(false)

	time.Sleep(30 * time.Millisecond)
	select {
	case <-ran:
		t.Fatal("索引仍在重建时不应执行维护回调")
	default:
	}

	filmsnapshot.NotifySearchIndexProgress(0, 0, "idle")
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runMaintenance error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("检索空闲后 runMaintenance 未恢复执行")
	}
	select {
	case <-ran:
	default:
		t.Fatal("维护回调未执行")
	}
}

// Review Fix 2：无分类早退与维护中止早退都必须在 batchCtx.sourceSnapshots 中留下真实状态。
func TestHandleCollectEarlyExitsRetainBatchSnapshot(t *testing.T) {
	resetSearchReseal()
	resetStreamPublishBufferForTest()
	t.Cleanup(func() {
		resetStreamPublishBufferForTest()
		resetSearchReseal()
	})
	gdb := setupAuditSpiderDB(t)
	sNoCat := model.FilmSource{Id: "early-nocat", Name: "无分类站", Uri: "http://127.0.0.1/nocat", State: true}
	sMaint := model.FilmSource{Id: "early-maint", Name: "维护站", Uri: "http://127.0.0.1/maint", State: true}
	if err := gdb.Create([]model.FilmSource{sNoCat, sMaint}).Error; err != nil {
		t.Fatal(err)
	}

	claim, err := claimCollectSources([]model.FilmSource{sNoCat, sMaint}, model.CollectTagBatchCollect)
	if err != nil {
		t.Fatal(err)
	}
	batch := newCollectBatchContext(model.CollectTriggerManual, model.CollectTagBatchCollect, claim.Sources, time.Now()).withClaim(claim)
	defer batch.close()

	ver := stopAllVersion.Load()
	if err := handleCollectWithStopVersion(sNoCat.Id, 24, &ver, batch); err == nil {
		t.Fatal("无分类站点应返回错误")
	}
	if err := collectLifecycle.beginDrain(time.Second); err != nil {
		t.Fatalf("beginDrain failed: %v", err)
	}
	_ = handleCollectWithStopVersion(sMaint.Id, 24, &ver, batch)
	collectLifecycle.endDrain()

	batch.mu.Lock()
	snapNoCat := batch.sourceSnapshots[sNoCat.Id]
	snapMaint := batch.sourceSnapshots[sMaint.Id]
	batch.mu.Unlock()
	if snapNoCat.Status != progress.StatusFailed || snapNoCat.Error == "" {
		t.Fatalf("无分类早退应保留 failed 快照，got=%+v", snapNoCat)
	}
	if snapMaint.Status != progress.StatusStopped {
		t.Fatalf("维护中止早退应保留 stopped 快照，got=%+v", snapMaint)
	}
}
