package service

import (
	"testing"

	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/repository/support"
	"server/internal/spider/progress"
)

func TestSwitchPrimary(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "src_1", Name: "站1", Uri: "http://1", Sort: 0, State: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "src_2", Name: "站2", Uri: "http://2", Sort: 1, State: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "src_3", Name: "停用站", Uri: "http://3", Sort: 2, State: false}).Error; err != nil {
		t.Fatal(err)
	}

	srv := &CollectService{}
	// 建立初始主站 src_1
	if p := srv.CurrentPrimary(); p == nil || p.Id != "src_1" {
		t.Fatalf("expected initial primary src_1, got %#v", p)
	}

	// 切换到 src_2
	if err := srv.SwitchPrimary("src_2"); err != nil {
		t.Fatalf("SwitchPrimary failed: %v", err)
	}
	if p := srv.CurrentPrimary(); p == nil || p.Id != "src_2" {
		t.Fatalf("expected primary src_2, got %#v", p)
	}
	list, err := srv.GetFilmSourceList()
	if err != nil {
		t.Fatalf("GetFilmSourceList failed: %v", err)
	}
	if len(list) < 2 || list[0].Id != "src_2" || list[0].Sort != 0 {
		t.Fatalf("expected src_2 at first place with sort 0, got %#v", list)
	}
	if list[1].Id != "src_1" || list[1].Sort != 1 {
		t.Fatalf("expected src_1 shifted to second place with sort 1, got %#v", list)
	}
	// 等切换触发的索引预热结束，避免后续用例撞上忙碌闸门
	waitSearchWorkIdle(t)

	// 幂等：重复切换同一站
	if err := srv.SwitchPrimary("src_2"); err != nil {
		t.Fatalf("switch to same primary should be idempotent, err=%v", err)
	}

	// 目标无效
	if err := srv.SwitchPrimary("ghost"); err == nil || err.Error() != "新主站不存在" {
		t.Fatalf("err=%v, want 新主站不存在", err)
	}
	if err := srv.SwitchPrimary("src_3"); err == nil || err.Error() != "新主站必须处于启用状态" {
		t.Fatalf("err=%v, want 新主站必须处于启用状态", err)
	}
	if err := srv.SwitchPrimary(""); err == nil || err.Error() != "主站 ID 不能为空" {
		t.Fatalf("err=%v, want 主站 ID 不能为空", err)
	}

	// 检索忙碌时拒绝切换
	primaryWarmCount.Add(1)
	t.Cleanup(func() { primaryWarmCount.Add(-1) })
	if err := srv.SwitchPrimary("src_1"); err == nil || err.Error() != filmsnapshot.ErrSearchIndexBusy.Error() {
		t.Fatalf("err=%v, want %v", err, filmsnapshot.ErrSearchIndexBusy)
	}
	if p := repository.CurrentPrimarySource(); p == nil || p.Id != "src_2" {
		t.Fatalf("rejected switch must keep primary src_2, got %#v", p)
	}
}

// 验证当删除队列已满时，删除当前主站被拒绝，且主站绝不被提前交接或撕裂
func TestTriggerDelFilmSource_QueueFullPreservesPrimary(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "p_src", Name: "原主站", Uri: "http://p", Sort: 0, State: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "new_src", Name: "新备选站", Uri: "http://n", Sort: 1, State: true}).Error; err != nil {
		t.Fatal(err)
	}
	_ = repository.RefreshCollectSourceCatalog()

	srv := &CollectService{}
	if p := srv.CurrentPrimary(); p == nil || p.Id != "p_src" {
		t.Fatalf("expected initial primary p_src, got %#v", p)
	}

	// 模拟队列已满
	origQueue := deleteSourceQueue
	fullQueue := make(chan deleteSourceJob, 1)
	fullQueue <- deleteSourceJob{id: "dummy_blocking_job"}
	deleteSourceQueue = fullQueue
	t.Cleanup(func() {
		deleteSourceQueue = origQueue
	})

	// 触发删除当前主站并尝试交接给 new_src
	err := srv.TriggerDelFilmSource("p_src", "new_src", false)
	if err == nil || err.Error() != "删除队列已满" {
		t.Fatalf("expected '删除队列已满', got err=%v", err)
	}

	// 断言：由于入队失败，主站依然是原主站 p_src，绝未被修改为 new_src
	if p := srv.CurrentPrimary(); p == nil || p.Id != "p_src" {
		t.Fatalf("primary must NOT be altered when enqueue fails, got %#v", p)
	}
}

func TestTriggerDelFilmSource_ReservationRollbackOnHandoverError(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)

	s1 := model.FilmSource{Id: "src_err_1", Name: "源站1", State: true, Uri: "https://s1.test"}
	if err := gdb.Create(&s1).Error; err != nil {
		t.Fatalf("create source failed: %v", err)
	}
	repository.RefreshCollectSourceCatalog()

	srv := &CollectService{}
	if err := srv.SwitchPrimary("src_err_1"); err != nil {
		t.Fatalf("set primary failed: %v", err)
	}
	waitSearchWorkIdle(t)

	// 传入非法的新主站 ID 导致 planPrimaryHandover 报错
	err := srv.TriggerDelFilmSource("src_err_1", "not_exist_source", false)
	if err == nil {
		t.Fatal("expected error for non-existent target new primary")
	}

	// 校验预占状态已成功回滚，未残留 deleting 标记
	if progress.IsSourceDeleting("src_err_1") {
		t.Fatal("source must NOT remain in deleting state when handover fails")
	}
}

func TestCancelDelFilmSources_SkipsQueuedSources(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)

	s1 := model.FilmSource{Id: "src_cancel_1", Name: "待删1", State: true, Uri: "https://c1.test"}
	s2 := model.FilmSource{Id: "src_cancel_2", Name: "待删2", State: true, Uri: "https://c2.test"}
	if err := gdb.Create(&s1).Error; err != nil {
		t.Fatalf("create s1 failed: %v", err)
	}
	if err := gdb.Create(&s2).Error; err != nil {
		t.Fatalf("create s2 failed: %v", err)
	}
	repository.RefreshCollectSourceCatalog()

	// 模拟已预占入队状态
	progress.SetSourceDeleting("src_cancel_1", true)
	progress.SetSourceDeleting("src_cancel_2", true)

	srv := &CollectService{}
	// 取消 src_cancel_2
	canceled := srv.CancelDelFilmSources([]string{"src_cancel_2"})
	if len(canceled) != 1 || canceled[0] != "src_cancel_2" {
		t.Fatalf("expected src_cancel_2 canceled, got %v", canceled)
	}

	// 断言：src_cancel_2 的 deleting 状态立即被解除
	if progress.IsSourceDeleting("src_cancel_2") {
		t.Fatal("src_cancel_2 must NOT be in deleting state after cancel")
	}
	// 断言：未取消的 src_cancel_1 依然在 deleting
	if !progress.IsSourceDeleting("src_cancel_1") {
		t.Fatal("src_cancel_1 must remain in deleting state")
	}

	// 模拟全部取消（空参数取消所有未开始执行的排队站）
	allCanceled := srv.CancelDelFilmSources(nil)
	if len(allCanceled) != 1 || allCanceled[0] != "src_cancel_1" {
		t.Fatalf("expected src_cancel_1 canceled in batch cancel, got %v", allCanceled)
	}
	if progress.IsSourceDeleting("src_cancel_1") {
		t.Fatal("src_cancel_1 must NOT be in deleting state after batch cancel")
	}
}

// 验证缓存冷态下删除当前主站不会因提前置位 deleting 导致 oldPrimary 误判为 nil 而跳过交接校验
func TestTriggerDelFilmSource_ColdCacheStillEnforcesHandover(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "cold_p", Name: "冷缓存主站", Uri: "http://p", Sort: 0, State: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "cold_b", Name: "备选站", Uri: "http://b", Sort: 1, State: true}).Error; err != nil {
		t.Fatal(err)
	}
	_ = repository.RefreshCollectSourceCatalog()

	srv := &CollectService{}
	if err := srv.SwitchPrimary("cold_p"); err != nil {
		t.Fatalf("SwitchPrimary failed: %v", err)
	}
	waitSearchWorkIdle(t)

	// 刻意清空主站内存缓存，模拟其它写操作刚触发 InvalidatePrimarySourceCache()
	support.InvalidatePrimarySourceCache()

	if err := srv.TriggerDelFilmSource("cold_p", "", false); err == nil {
		t.Fatal("expected handover error when deleting primary without newPrimaryID even with cold cache")
	}
	if progress.IsSourceDeleting("cold_p") {
		t.Fatal("cold_p must not remain in deleting state after rejection")
	}
	if err := srv.TriggerDelFilmSourceBatch([]string{"cold_p", "cold_b"}, "", false); err == nil {
		t.Fatal("expected confirmClearPrimary error in batch delete with cold cache")
	}
}

// 验证 reserve 与 commit 之间并发撤回不会导致站点重新入队或卡在删除态，且旧令牌跳过不误伤新入队任务
func TestCancelDelFilmSources_ReservationAndTokenIsolation(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	s := model.FilmSource{Id: "src_tok", Name: "令牌站", State: true, Uri: "https://tok.test"}
	if err := gdb.Create(&s).Error; err != nil {
		t.Fatal(err)
	}

	mockQ := make(chan deleteSourceJob, 8)
	res, err := reserveDelFilmSources(mockQ, []*model.FilmSource{&s})
	if err != nil {
		t.Fatalf("reserveDelFilmSources failed: %v", err)
	}

	srv := &CollectService{}
	// 在 commit 之前并发调用撤回
	canceled := srv.CancelDelFilmSources([]string{s.Id})
	if len(canceled) != 1 || canceled[0] != s.Id {
		t.Fatalf("expected canceled=[%s], got %v", s.Id, canceled)
	}
	if progress.IsSourceDeleting(s.Id) {
		t.Fatal("source deleting flag must be cleared immediately after cancel")
	}

	// 随后执行 commit，已被取消的预占任务不得再次入队也不得恢复 deleting 标记
	res.commit(nil, "")
	if len(mockQ) != 0 {
		t.Fatalf("canceled reservation must not enqueue job, len=%d", len(mockQ))
	}
	if progress.IsSourceDeleting(s.Id) {
		t.Fatal("commit after cancel must not re-enable deleting flag")
	}

	// 验证旧取消令牌出队时不会清除后来重新入队任务的状态
	if err := enqueueDelFilmSources(mockQ, []*model.FilmSource{&s}); err != nil {
		t.Fatalf("first enqueue failed: %v", err)
	}
	firstJob := <-mockQ
	deleteSourceJobs.Done()
	// 放回队列中模拟尚未消费时被取消，再重新入队新任务
	deleteSourceStateMu.Lock()
	queuedDeleteJobs[s.Id] = firstJob.token
	deleteSourceStateMu.Unlock()
	if got := srv.CancelDelFilmSources([]string{s.Id}); len(got) != 1 {
		t.Fatalf("expected first job canceled, got %v", got)
	}
	if err := enqueueDelFilmSources(mockQ, []*model.FilmSource{&s}); err != nil {
		t.Fatalf("second enqueue failed: %v", err)
	}
	secondJob := <-mockQ
	deleteSourceJobs.Done()
	if secondJob.token == firstJob.token {
		t.Fatal("expected distinct token for re-enqueued job")
	}

	// 旧已取消令牌出队跳过时，不得把第二轮任务的 deleting=true 误清
	processAsyncDeleteSource(firstJob)
	if !progress.IsSourceDeleting(s.Id) {
		t.Fatal("stale canceled token must NOT clear deleting state of newly enqueued job")
	}
	// 清理测试队列状态
	deleteSourceStateMu.Lock()
	delete(queuedDeleteJobs, s.Id)
	deleteSourceStateMu.Unlock()
	progress.SetSourceDeleting(s.Id, false)
}

// 验证批量含主站置空模式下，若在非主站清理期间撤回队列，主站状态不会被提前置空
func TestTriggerDelFilmSourceBatch_CancelPreservesPrimaryWhenConfirmClear(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	if err := gdb.Create(&model.FilmSource{Id: "disabled_first", Name: "停用站", Uri: "http://d", Sort: 1, State: false}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "only_primary", Name: "唯一主站", Uri: "http://p", Sort: 0, State: true}).Error; err != nil {
		t.Fatal(err)
	}
	_ = repository.RefreshCollectSourceCatalog()

	srv := &CollectService{}
	if err := srv.SwitchPrimary("only_primary"); err != nil {
		t.Fatalf("SwitchPrimary failed: %v", err)
	}
	waitSearchWorkIdle(t)

	// 用隔离缓冲队列拦截入队任务，模拟主站排在队尾尚未出队时用户发起撤回
	origQueue := deleteSourceQueue
	testQ := make(chan deleteSourceJob, 10)
	deleteSourceQueue = testQ
	t.Cleanup(func() {
		for len(testQ) > 0 {
			<-testQ
			deleteSourceJobs.Done()
		}
		deleteSourceQueue = origQueue
		deleteSourceStateMu.Lock()
		delete(queuedDeleteJobs, "disabled_first")
		delete(queuedDeleteJobs, "only_primary")
		deleteSourceStateMu.Unlock()
		progress.SetSourceDeleting("disabled_first", false)
		progress.SetSourceDeleting("only_primary", false)
	})

	if err := srv.TriggerDelFilmSourceBatch([]string{"disabled_first", "only_primary"}, "", true); err != nil {
		t.Fatalf("TriggerDelFilmSourceBatch failed: %v", err)
	}
	// 撤销排队中的唯一主站
	canceled := srv.CancelDelFilmSources([]string{"only_primary"})
	if len(canceled) != 1 || canceled[0] != "only_primary" {
		t.Fatalf("expected only_primary canceled, got %v", canceled)
	}
	// 队列撤回后，唯一主站仍完整保留（未被提前置空）
	if stored := repository.StoredPrimarySourceID(); stored != "only_primary" {
		t.Fatalf("expected stored primary preserved as only_primary, got %q", stored)
	}
	if cur := srv.CurrentPrimary(); cur == nil || cur.Id != "only_primary" {
		t.Fatalf("expected CurrentPrimary to remain only_primary after cancel, got %#v", cur)
	}
	// 抵消入队时增加的 WaitGroup 计数，避免影响 waitSearchWorkIdle
	for len(testQ) > 0 {
		<-testQ
		deleteSourceJobs.Done()
	}
}
