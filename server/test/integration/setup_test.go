// Package integration 承载跨模块、全链路集成测试。
//
// 约定：
//   - 仅通过 internal 包的公开 API 黑盒驱动，不触碰未导出符号；
//   - 每个用例自包含 DB/Redis 初始化与销毁，用例之间互不影响；
//   - 慢或重负荷的场景放 test/benchmark，本包只保留可重复的功能链路验证。
package integration

import (
	"fmt"
	"strings"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	filmsnapshot "server/internal/repository/film/snapshot"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newTestDB 打开用例独占的内存 sqlite 并接管 db.Mdb，默认关闭 Redis 缓存。
// 用例结束（含所有子 cleanup）后还原全局态，并等待索引/缓存后台协程退出。
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:it_%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开内存 sqlite 失败: %v", err)
	}
	if err := gdb.AutoMigrate(model.AllModels...); err != nil {
		t.Fatalf("迁移测试库结构失败: %v", err)
	}

	origMdb := db.Mdb
	origRdb := db.Rdb
	db.Mdb = gdb
	db.Rdb = nil

	// 先注册 → 最后执行：等后台协程收尾后再还原全局句柄
	t.Cleanup(func() {
		filmsnapshot.WaitActiveFilmSearchIndexBuilt()
		db.Mdb = origMdb
		db.Rdb = origRdb
	})
	return gdb
}

// useRedis 用 miniredis 接管 db.Rdb，用例结束后还原并关闭。
func useRedis(t *testing.T) *redis.Client {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	orig := db.Rdb
	db.Rdb = client

	t.Cleanup(func() {
		_ = client.Close()
		mr.Close()
		db.Rdb = orig
	})
	return client
}

// activateVersion 激活快照版本并等待内存检索索引就绪；用例结束后清空读模型与版本态。
func activateVersion(t *testing.T, version string) {
	t.Helper()

	if err := filmsnapshot.SetActiveSnapshotVersion(version); err != nil {
		t.Fatalf("设置活跃快照版本失败: %v", err)
	}
	if err := filmsnapshot.LoadActiveFilmReadModel(version); err != nil {
		t.Fatalf("加载活跃读模型失败: %v", err)
	}
	filmsnapshot.WaitActiveFilmSearchIndexBuilt()

	t.Cleanup(func() {
		filmsnapshot.WaitActiveFilmSearchIndexBuilt()
		filmsnapshot.ClearActiveFilmReadModel()
		filmsnapshot.ResetActiveSnapshotFallbackForTest()
		filmsnapshot.ResetSearchCacheVersionForTest()
	})
}

// seedSnapshots 按指定版本写入快照数据。
func seedSnapshots(t *testing.T, gdb *gorm.DB, version string, rows ...model.FilmListSnapshot) {
	t.Helper()

	for _, row := range rows {
		row.SnapshotVersion = version
		if err := gdb.Create(&row).Error; err != nil {
			t.Fatalf("写入快照失败: %v", err)
		}
	}
}

// searchFilms 执行前台关键词检索，返回命中的快照与分页态。
func searchFilms(version, keyword string, pageSize int) ([]model.FilmListSnapshot, *dto.Page) {
	page := &dto.Page{Current: 1, PageSize: pageSize}
	rows := filmsnapshot.SearchSnapshotsByKeywordAndSortReadModel(version, keyword, "", page)
	return rows, page
}

// midSet 提取检索结果中的 mid 集合，便于断言命中/未命中。
func midSet(rows []model.FilmListSnapshot) map[int64]bool {
	got := make(map[int64]bool, len(rows))
	for _, row := range rows {
		got[row.Mid] = true
	}
	return got
}

// assertHit 断言检索结果恰好命中期望的 mid 集合。
func assertHit(t *testing.T, scene, keyword string, rows []model.FilmListSnapshot, page *dto.Page, wantMids ...int64) {
	t.Helper()

	got := midSet(rows)
	if len(got) != len(wantMids) {
		t.Fatalf("%s：关键词 %q 命中 %d 条（mids=%v），期望 %d 条（mids=%v）", scene, keyword, len(got), got, len(wantMids), wantMids)
	}
	for _, mid := range wantMids {
		if !got[mid] {
			t.Fatalf("%s：关键词 %q 未命中期望 mid=%d（实际 mids=%v）", scene, keyword, mid, got)
		}
	}
	if page.Total != len(wantMids) {
		t.Fatalf("%s：关键词 %q 的 page.Total=%d，期望 %d", scene, keyword, page.Total, len(wantMids))
	}
}

// assertMiss 断言检索结果为空且分页态合法。
func assertMiss(t *testing.T, scene, keyword string, rows []model.FilmListSnapshot, page *dto.Page) {
	t.Helper()

	if len(rows) != 0 || page.Total != 0 {
		t.Fatalf("%s：关键词 %q 期望 0 条，实际 %d 条（total=%d）", scene, keyword, len(rows), page.Total)
	}
}
