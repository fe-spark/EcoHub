package snapshot

import (
	"testing"
	"time"

	"server/internal/model"
	"server/internal/model/dto"

	"gorm.io/gorm"
)

func TestListProvideSnapshots_TagFilter(t *testing.T) {
	gdb := setupOthersFilterTestDB(t)
	version := "test_provide_filter_v1"
	const targetPid int64 = 9

	if err := gdb.Create(&model.Category{Id: targetPid, Pid: 0, Name: "电影", Show: true, Sort: 1}).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	snapshots := []model.FilmListSnapshot{
		{SnapshotVersion: version, Mid: 1, Pid: targetPid, Name: "大陆动作片", Area: "中国大陆", Year: 2024, ClassTag: "动作"},
		{SnapshotVersion: version, Mid: 2, Pid: targetPid, Name: "美国动作片", Area: "美国", Year: 2023, ClassTag: "动作"},
		{SnapshotVersion: version, Mid: 3, Pid: targetPid, Name: "大陆喜剧片", Area: "中国大陆", Year: 2024, ClassTag: "喜剧"},
	}
	for _, s := range snapshots {
		if err := gdb.Create(&s).Error; err != nil {
			t.Fatalf("create snapshot: %v", err)
		}
	}

	page := &dto.Page{Current: 1, PageSize: 10}
	res := ListProvideSnapshotsReadModel(version, model.SearchTagsVO{Pid: targetPid, Area: "中国大陆"}, "", 0, page)
	if len(res) != 2 || page.Total != 2 {
		t.Fatalf("Area=中国大陆: expected 2, got len=%d total=%d", len(res), page.Total)
	}

	page2 := &dto.Page{Current: 1, PageSize: 10}
	res2 := ListProvideSnapshotsReadModel(version, model.SearchTagsVO{Pid: targetPid, Area: "中国大陆", Plot: "动作"}, "", 0, page2)
	if len(res2) != 1 || page2.Total != 1 || res2[0].Mid != 1 {
		t.Fatalf("Area+Plot: expected only Mid=1, got %+v total=%d", res2, page2.Total)
	}

	page3 := &dto.Page{Current: 1, PageSize: 10}
	res3 := ListProvideSnapshotsReadModel(version, model.SearchTagsVO{Pid: targetPid, Area: "美国"}, "动作片", 0, page3)
	if len(res3) != 1 || page3.Total != 1 || res3[0].Mid != 2 {
		t.Fatalf("keyword+Area: expected only Mid=2, got %+v total=%d", res3, page3.Total)
	}
}

func TestGetSnapshotByMid_UnscopedIncludesSoftDeleted(t *testing.T) {
	gdb := setupOthersFilterTestDB(t)
	version := "test_provide_unscoped_v1"
	s := model.FilmListSnapshot{SnapshotVersion: version, Mid: 11, Name: "软删快照"}
	if err := gdb.Create(&s).Error; err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	if err := gdb.Unscoped().Model(&model.FilmListSnapshot{}).Where("mid = ?", 11).Update("deleted_at", time.Now()).Error; err != nil {
		t.Fatalf("mark deleted_at: %v", err)
	}

	var scoped model.FilmListSnapshot
	if err := gdb.Where("snapshot_version = ? AND mid = ?", version, int64(11)).First(&scoped).Error; err == nil {
		t.Fatal("scoped query should hide soft-deleted snapshot")
	} else if err != gorm.ErrRecordNotFound {
		t.Fatalf("scoped query unexpected err: %v", err)
	}

	got := GetSnapshotByMid(version, 11)
	if got == nil || got.Mid != 11 {
		t.Fatalf("Unscoped point lookup should still return snapshot, got %+v", got)
	}

	rows := GetSnapshotsByMidsOrdered(version, []int64{11})
	if len(rows) != 1 || rows[0].Mid != 11 {
		t.Fatalf("Unscoped batch lookup should still return snapshot, got %+v", rows)
	}
}
