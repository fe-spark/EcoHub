package service

import (
	"testing"

	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
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
	if p := repository.PickPrimarySourceForCategory(); p == nil || p.Id != "src_2" {
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
	fullQueue := make(chan string, 1)
	fullQueue <- "dummy_blocking_job"
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
