package film

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupSnapshotRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s_%d?mode=memory&cache=shared&_busy_timeout=5000", t.Name(), time.Now().UnixNano())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := gdb.AutoMigrate(
		&model.FilmIndex{},
		&model.MovieDetailInfo{},
		&model.FilmListSnapshot{},
		&model.FilmFilterOptionSnapshot{},
		&model.Category{},
		&model.SlaveMoviePlaylist{},
		&model.MovieMatchKey{},
		&model.FilmSource{},
	); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	origMdb := db.Mdb
	origRdb := db.Rdb
	db.Mdb = gdb
	db.Rdb = nil
	ResetActiveSnapshotFallbackForTest()
	ClearActiveFilmReadModel()

	t.Cleanup(func() {
		WaitActiveFilmSearchIndexBuilt()
		db.Mdb = origMdb
		db.Rdb = origRdb
		ResetActiveSnapshotFallbackForTest()
		ClearActiveFilmReadModel()
	})

	return gdb
}

func createTestFilm(t *testing.T, gdb *gorm.DB, mid int64, name string, updateStamp int64, playSummary string) {
	t.Helper()
	detail := model.MovieDetail{
		Id:       mid,
		Name:     name,
		PlayList: [][]model.MovieUrlInfo{{{Episode: "第1集", Link: "https://test.com/1.m3u8"}}},
		PlayFrom: []string{"默认主源"},
	}
	contentJSON, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	detailInfo := model.MovieDetailInfo{
		Mid:     mid,
		Content: string(contentJSON),
	}
	if err := gdb.Create(&detailInfo).Error; err != nil {
		t.Fatalf("create detail: %v", err)
	}

	index := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid:        mid,
			ContentKey: fmt.Sprintf("key_%d", mid),
			SourceId:   "source_master",
		},
		FilmIndexCategory: model.FilmIndexCategory{
			Pid:   1,
			Cid:   10,
			CName: "动作片",
		},
		FilmIndexContent: model.FilmIndexContent{
			Name:        name,
			UpdateStamp: updateStamp,
		},
		FilmIndexDerived: model.FilmIndexDerived{
			PlayFromSummary: playSummary,
		},
	}
	if err := gdb.Create(&index).Error; err != nil {
		t.Fatalf("create film index: %v", err)
	}
}

func TestEnsureActiveFilmListSnapshot_EmptyDB(t *testing.T) {
	_ = setupSnapshotRepoTestDB(t)

	if err := EnsureActiveFilmListSnapshot(); err != nil {
		t.Fatalf("expected nil error on empty DB, got: %v", err)
	}
	if ver := GetActiveSnapshotVersion(); ver != "" {
		t.Fatalf("expected empty snapshot version for empty DB, got %q", ver)
	}
}

func TestEnsureActiveFilmListSnapshot_InitialBuild(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	createTestFilm(t, gdb, 101, "测试影片1", 1000, "默认主源")

	if err := EnsureActiveFilmListSnapshot(); err != nil {
		t.Fatalf("EnsureActiveFilmListSnapshot: %v", err)
	}

	ver := GetActiveSnapshotVersion()
	if ver == "" {
		t.Fatal("expected non-empty active snapshot version after initial build")
	}

	var count int64
	if err := gdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", ver).Count(&count).Error; err != nil {
		t.Fatalf("count snapshot: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 snapshot record, got %d", count)
	}

	rm := GetActiveFilmReadModel()
	if rm == nil || rm.Version != ver {
		t.Fatalf("expected active read model version %q, got %+v", ver, rm)
	}
}

func TestEnsureActiveFilmListSnapshot_GhostSnapshotSelfHealing(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	createTestFilm(t, gdb, 101, "测试影片1", 1000, "默认主源")

	const ghostVer = "ghost_ver_999"
	_ = SetActiveSnapshotVersion(ghostVer)

	var snapCount int64
	_ = gdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", ghostVer).Count(&snapCount)
	if snapCount != 0 {
		t.Fatalf("expected 0 snapshots for ghost version, got %d", snapCount)
	}

	if err := EnsureActiveFilmListSnapshot(); err != nil {
		t.Fatalf("EnsureActiveFilmListSnapshot: %v", err)
	}

	newVer := GetActiveSnapshotVersion()
	if newVer == "" || newVer == ghostVer {
		t.Fatalf("expected ghost version %q to be replaced by new version, got %q", ghostVer, newVer)
	}

	var newSnapCount int64
	if err := gdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", newVer).Count(&newSnapCount).Error; err != nil {
		t.Fatalf("count new snapshot: %v", err)
	}
	if newSnapCount != 1 {
		t.Fatalf("expected 1 snapshot record for new version, got %d", newSnapCount)
	}

	rm := GetActiveFilmReadModel()
	if rm == nil || rm.Version != newVer {
		t.Fatalf("expected read model version %q, got %+v", newVer, rm)
	}
}

func TestEnsureActiveFilmListSnapshot_CountMismatch(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	createTestFilm(t, gdb, 101, "测试影片1", 1000, "默认主源")
	createTestFilm(t, gdb, 102, "测试影片2", 1000, "默认主源")

	// 模拟上次停机前快照只包含 1 部影片（异常强杀导致快照数量落后）
	const oldVer = "old_ver_partial"
	snap1 := model.FilmListSnapshot{
		SnapshotVersion: oldVer,
		Mid:             101,
		Name:            "测试影片1",
		UpdateStamp:     1000,
	}
	if err := gdb.Create(&snap1).Error; err != nil {
		t.Fatalf("create partial snapshot: %v", err)
	}
	_ = SetActiveSnapshotVersion(oldVer)

	// 验证触发前 dbCount(2) != snapshotCount(1)
	if err := EnsureActiveFilmListSnapshot(); err != nil {
		t.Fatalf("EnsureActiveFilmListSnapshot: %v", err)
	}

	newVer := GetActiveSnapshotVersion()
	if newVer == "" || newVer == oldVer {
		t.Fatalf("expected old partial version %q to be rebuilt, got %q", oldVer, newVer)
	}

	var healedCount int64
	if err := gdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", newVer).Count(&healedCount).Error; err != nil {
		t.Fatalf("count healed snapshot: %v", err)
	}
	if healedCount != 2 {
		t.Fatalf("expected 2 snapshot records after rebuilding, got %d", healedCount)
	}
}

func TestEnsureActiveFilmListSnapshot_ClearOnShutdownAndRebuildOnStartup(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	createTestFilm(t, gdb, 101, "测试影片1", 1000, "默认主源")

	// 首次启动建快照
	if err := EnsureActiveFilmListSnapshot(); err != nil {
		t.Fatalf("first EnsureActiveFilmListSnapshot: %v", err)
	}
	WaitActiveFilmSearchIndexBuilt()
	ver1 := GetActiveSnapshotVersion()
	if ver1 == "" {
		t.Fatal("expected non-empty version")
	}

	// 采集写入了新影片 102
	createTestFilm(t, gdb, 102, "测试影片2", 1000, "默认主源")

	// 退出服务：快速清空快照版本号
	ClearSnapshotState()
	if ver := GetActiveSnapshotVersion(); ver != "" {
		t.Fatalf("expected empty active version after ClearSnapshotState, got %q", ver)
	}

	// 重新启动：检测到版本号为空，秒级自动收尾，全部影片立刻激活
	if err := EnsureActiveFilmListSnapshot(); err != nil {
		t.Fatalf("restart EnsureActiveFilmListSnapshot: %v", err)
	}
	WaitActiveFilmSearchIndexBuilt()

	ver2 := GetActiveSnapshotVersion()
	if ver2 == "" || ver2 == ver1 {
		t.Fatalf("expected new active snapshot version, got %q", ver2)
	}

	var totalSnapCount int64
	if err := gdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", ver2).Count(&totalSnapCount).Error; err != nil {
		t.Fatalf("count snapshot: %v", err)
	}
	if totalSnapCount != 2 {
		t.Fatalf("expected 2 snapshot records after restart, got %d", totalSnapCount)
	}
}

func TestEnsureActiveFilmListSnapshot_ConsistentNoRebuild(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	rootCat := model.Category{Id: 1, Pid: 0, Name: "电影", Show: true}
	_ = gdb.Create(&rootCat)

	createTestFilm(t, gdb, 101, "测试影片1", 1000, "默认主源")

	// 首次构建
	if err := EnsureActiveFilmListSnapshot(); err != nil {
		t.Fatalf("first EnsureActiveFilmListSnapshot: %v", err)
	}

	firstVer := GetActiveSnapshotVersion()
	if firstVer == "" {
		t.Fatal("expected valid version after first build")
	}

	// 再次调用，数据完全一致，不应发生重建
	if err := EnsureActiveFilmListSnapshot(); err != nil {
		t.Fatalf("second EnsureActiveFilmListSnapshot: %v", err)
	}

	secondVer := GetActiveSnapshotVersion()
	if secondVer != firstVer {
		t.Fatalf("consistent state must not trigger rebuild; version changed from %q to %q", firstVer, secondVer)
	}
}

func TestEnsureActiveFilmListSnapshot_PlayFromSummaryHealed(t *testing.T) {
	gdb := setupSnapshotRepoTestDB(t)
	// 影片入库时 play_from_summary 为空
	createTestFilm(t, gdb, 201, "待收尾影片", 1500, "")

	var origIndex model.FilmIndex
	_ = gdb.Where("mid = ?", 201).First(&origIndex)
	if origIndex.PlayFromSummary != "" {
		t.Fatalf("expected initial empty play_from_summary, got %q", origIndex.PlayFromSummary)
	}

	if err := EnsureActiveFilmListSnapshot(); err != nil {
		t.Fatalf("EnsureActiveFilmListSnapshot: %v", err)
	}

	ver := GetActiveSnapshotVersion()
	if ver == "" {
		t.Fatal("expected non-empty active version")
	}

	// 验证 film_index 与快照表中的 play_from_summary 均已自愈刷新
	var healedIndex model.FilmIndex
	_ = gdb.Where("mid = ?", 201).First(&healedIndex)
	if healedIndex.PlayFromSummary == "" {
		t.Fatal("expected film_index play_from_summary to be refreshed during self-healing")
	}

	var healedSnap model.FilmListSnapshot
	_ = gdb.Where("snapshot_version = ? AND mid = ?", ver, 201).First(&healedSnap)
	if healedSnap.PlayFromSummary == "" {
		t.Fatal("expected film_list_snapshot play_from_summary to be refreshed during self-healing")
	}
}
