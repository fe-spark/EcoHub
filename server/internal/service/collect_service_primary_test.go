package service

import (
	"testing"

	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
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
