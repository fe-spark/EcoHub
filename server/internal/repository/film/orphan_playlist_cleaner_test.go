package film

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupOrphanCleanerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	ClearMasterSwitchProtection()
	t.Cleanup(ClearMasterSwitchProtection)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_busy_timeout=5000", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := gdb.AutoMigrate(
		&model.FilmSource{},
		&model.FilmListSnapshot{},
		&model.MovieMatchKey{},
		&model.MoviePlaylist{},
		&model.SlaveMoviePlaylist{},
		&model.MovieDetailInfo{},
		&model.FilmIndex{},
	); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	db.Mdb = gdb
	return gdb
}

func TestCleanOrphanPlaylists_Gates(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	// 1. 无快照
	deleted, err := CleanOrphanPlaylists()
	if err != nil {
		t.Fatalf("unexpected error without snapshot: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("expected 0 deleted without snapshot, got %d", deleted)
	}

	// 2. 有快照，但无 match_key
	snap := model.FilmListSnapshot{SnapshotVersion: "v1", Mid: 101, Pid: 1}
	if err := gdb.Create(&snap).Error; err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	deleted, err = CleanOrphanPlaylists()
	if err != nil {
		t.Fatalf("unexpected error without match keys: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("expected 0 deleted without match keys, got %d", deleted)
	}
}

func TestCleanOrphanPlaylists_MarkAndRevive(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	// 快照与匹配键
	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: "v1", Mid: 101, Pid: 1})
	gdb.Create(&model.FilmSource{Id: "slave_1", Name: "附属站1", Grade: model.SlaveCollect, State: true})
	gdb.Create(&model.MovieMatchKey{Mid: 101, MatchKey: "valid_key"})

	// 1. 正常匹配的记录
	p1 := model.SlaveMoviePlaylist{ID: 1, SourceId: "slave_1", MovieKey: "valid_key"}
	// 2. 附属站抓到但主站暂时没有的记录（待定孤儿）
	p2 := model.SlaveMoviePlaylist{ID: 2, SourceId: "slave_1", MovieKey: "pending_key"}
	gdb.Create(&p1)
	gdb.Create(&p2)

	// 首次扫描：未匹配的 p2 应被打上待定孤儿标记（进入观察期），但不物理删除
	purged, err := CleanOrphanPlaylists()
	if err != nil {
		t.Fatalf("CleanOrphanPlaylists failed: %v", err)
	}
	if purged != 0 {
		t.Fatalf("expected 0 purged during observation period, got %d", purged)
	}

	var row1, row2 model.SlaveMoviePlaylist
	gdb.First(&row1, 1)
	gdb.First(&row2, 2)

	if row1.OrphanMarkedAt != nil {
		t.Fatalf("expected valid row1 OrphanMarkedAt to be nil, got %v", row1.OrphanMarkedAt)
	}
	if row2.OrphanMarkedAt == nil {
		t.Fatalf("expected orphan row2 OrphanMarkedAt to be marked, got nil")
	}

	// 模拟随后主站采集到了 pending_key，触发 ReviveSlavePlaylistsTx
	if err := ReviveSlavePlaylistsTx(gdb, []string{"pending_key"}); err != nil {
		t.Fatalf("ReviveSlavePlaylistsTx failed: %v", err)
	}

	// 验证 row2 已成功复活（OrphanMarkedAt 被清空）
	var revived model.SlaveMoviePlaylist
	if err := gdb.First(&revived, 2).Error; err != nil {
		t.Fatalf("query revived row2 failed: %v", err)
	}
	if revived.OrphanMarkedAt != nil {
		t.Fatalf("expected revived row2 OrphanMarkedAt to be nil, got %v", revived.OrphanMarkedAt)
	}
}

func TestCleanOrphanPlaylists_PurgeExpired(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: "v1", Mid: 101, Pid: 1})
	gdb.Create(&model.FilmSource{Id: "slave_1", Name: "附属站1", Grade: model.SlaveCollect, State: true})
	gdb.Create(&model.MovieMatchKey{Mid: 101, MatchKey: "valid_key"})

	now := time.Now()
	markedPast50h := now.Add(-50 * time.Hour)
	markedPast10h := now.Add(-10 * time.Hour)

	// 1. 超期 48 小时的真孤儿 -> 必须被物理回收
	p1 := model.SlaveMoviePlaylist{ID: 1, SourceId: "slave_1", MovieKey: "expired_orphan", OrphanMarkedAt: &markedPast50h}
	// 2. 刚刚标记 10 小时的待定孤儿 -> 在 48h 观察期内，安全保留
	p2 := model.SlaveMoviePlaylist{ID: 2, SourceId: "slave_1", MovieKey: "recent_orphan", OrphanMarkedAt: &markedPast10h}
	// 3. 正常匹配记录 -> 保留
	p3 := model.SlaveMoviePlaylist{ID: 3, SourceId: "slave_1", MovieKey: "valid_key"}

	gdb.Create(&p1)
	gdb.Create(&p2)
	gdb.Create(&p3)

	purged, err := CleanOrphanPlaylists()
	if err != nil {
		t.Fatalf("CleanOrphanPlaylists failed: %v", err)
	}
	if purged != 1 {
		t.Fatalf("expected 1 expired orphan purged, got %d", purged)
	}

	var remaining []model.SlaveMoviePlaylist
	gdb.Unscoped().Order("id ASC").Find(&remaining)
	if len(remaining) != 2 {
		t.Fatalf("expected 2 records remaining, got %d", len(remaining))
	}
	if remaining[0].ID != 2 || remaining[1].ID != 3 {
		t.Fatalf("expected IDs [2, 3] remaining, got [%d, %d]", remaining[0].ID, remaining[1].ID)
	}
}

func TestSlavePlaylistMigration_Idempotency(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	// 在老表中插入数据
	for i := uint(1); i <= 5; i++ {
		gdb.Create(&model.MoviePlaylist{
			Model:      gorm.Model{ID: i},
			SourceId:   "slave_1",
			MovieKey:   fmt.Sprintf("key_%d", i),
			GroupIndex: 0,
			GroupName:  "线路1",
			Content:    "[]",
		})
	}

	// 首次割接迁移
	if err := MigrateLegacyMoviePlaylistsTx(gdb); err != nil {
		t.Fatalf("first migration failed: %v", err)
	}

	var count, legacyCount int64
	gdb.Model(&model.SlaveMoviePlaylist{}).Count(&count)
	if count != 5 {
		t.Fatalf("expected 5 rows in slave_movie_playlists, got %d", count)
	}
	gdb.Model(&model.MoviePlaylist{}).Count(&legacyCount)
	if legacyCount != 0 {
		t.Fatalf("expected 0 rows in legacy movie_playlist after cutover, got %d", legacyCount)
	}

	// 第二次执行迁移，老表为空，应幂等跳过
	if err := MigrateLegacyMoviePlaylistsTx(gdb); err != nil {
		t.Fatalf("second migration failed: %v", err)
	}
	gdb.Model(&model.SlaveMoviePlaylist{}).Count(&count)
	if count != 5 {
		t.Fatalf("expected count still 5 after second migration, got %d", count)
	}
}

func TestSlavePlaylistMigration_ResumeInterrupted(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	// 模拟割接中断或爬虫已提前写入 2 条数据到新表
	gdb.Create(&model.SlaveMoviePlaylist{
		SourceId:   "slave_1",
		MovieKey:   "key_1",
		GroupIndex: 0,
		GroupName:  "线路1_新",
		Content:    "[]",
	})
	gdb.Create(&model.SlaveMoviePlaylist{
		SourceId:   "slave_1",
		MovieKey:   "key_2",
		GroupIndex: 0,
		GroupName:  "线路1_新",
		Content:    "[]",
	})

	// 老表中仍有 5 条数据等待迁移（包含 key_1~key_5）
	for i := uint(1); i <= 5; i++ {
		gdb.Create(&model.MoviePlaylist{
			Model:      gorm.Model{ID: i},
			SourceId:   "slave_1",
			MovieKey:   fmt.Sprintf("key_%d", i),
			GroupIndex: 0,
			GroupName:  fmt.Sprintf("线路1_老_%d", i),
			Content:    "[]",
		})
	}

	// 执行割接迁移：即使新表已有数据，也必须把老表剩余数据全部迁移并排空
	if err := MigrateLegacyMoviePlaylistsTx(gdb); err != nil {
		t.Fatalf("resume migration failed: %v", err)
	}

	var legacyCount, slaveCount int64
	gdb.Model(&model.MoviePlaylist{}).Count(&legacyCount)
	if legacyCount != 0 {
		t.Fatalf("expected legacy movie_playlist to be 0 after resumed migration, got %d", legacyCount)
	}

	gdb.Model(&model.SlaveMoviePlaylist{}).Count(&slaveCount)
	if slaveCount != 5 {
		t.Fatalf("expected 5 total records in slave_movie_playlists, got %d", slaveCount)
	}
}

func TestLoadExistingMatchKeySet_Chunking(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	// 插入 1200 个匹配键，超出 SQLite 默认 999 参数限制
	const total = 1200
	matchKeys := make([]model.MovieMatchKey, 0, total)
	queryKeys := make([]string, 0, total+300)
	for i := 1; i <= total; i++ {
		k := fmt.Sprintf("chunk_key_%d", i)
		matchKeys = append(matchKeys, model.MovieMatchKey{
			Mid:      int64(i),
			MatchKey: k,
		})
		queryKeys = append(queryKeys, k)
	}
	// 额外 300 个不存在的 key
	for i := total + 1; i <= total+300; i++ {
		queryKeys = append(queryKeys, fmt.Sprintf("non_existent_%d", i))
	}

	if err := gdb.CreateInBatches(matchKeys, 500).Error; err != nil {
		t.Fatalf("insert match keys: %v", err)
	}

	// 验证分块检索：不得抛出 "too many SQL variables"
	found, err := loadExistingMatchKeySet(queryKeys)
	if err != nil {
		t.Fatalf("loadExistingMatchKeySet failed with chunking: %v", err)
	}
	if len(found) != total {
		t.Fatalf("expected %d matched keys, got %d", total, len(found))
	}

	// 验证批量复活同样支持分块 (ReviveSlavePlaylistsTx)
	if err := ReviveSlavePlaylistsTx(gdb, queryKeys); err != nil {
		t.Fatalf("ReviveSlavePlaylistsTx failed: %v", err)
	}
}

func TestCleanOrphanPlaylists_CursorResume(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	ClearOrphanCleanCursor()
	defer ClearOrphanCleanCursor()

	// 准备主站快照与匹配键
	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: "v1", Mid: 1, Pid: 1})
	gdb.Create(&model.MovieMatchKey{Mid: 1, MatchKey: "key_matched"})

	// 插入 6 条附属站记录（ID 1..6）
	for i := uint(1); i <= 6; i++ {
		gdb.Create(&model.SlaveMoviePlaylist{
			ID:       i,
			SourceId: "slave_1",
			MovieKey: fmt.Sprintf("orphan_key_%d", i),
		})
	}

	// 临时修改批次大小为 2，方便断点测试
	origScanBatchSize := orphanPlaylistScanBatchSize
	origCooldown := orphanPlaylistBatchCooldown
	orphanPlaylistScanBatchSize = 2
	orphanPlaylistBatchCooldown = 0
	defer func() {
		orphanPlaylistScanBatchSize = origScanBatchSize
		orphanPlaylistBatchCooldown = origCooldown
	}()

	// 模拟运行并在处理完 1 批 (2 条) 后停止
	stopAfterOneBatch := func() bool {
		return LoadOrphanCleanCursor() >= 2
	}

	_, err := markCandidateOrphans(time.Now().Add(time.Minute), stopAfterOneBatch)
	if err != nil {
		t.Fatalf("partial scan failed: %v", err)
	}

	cur := LoadOrphanCleanCursor()
	if cur != 2 {
		t.Fatalf("expected cursor to be saved at 2, got %d", cur)
	}

	// 第二次执行：从游标 2 继续向后扫描直至扫完整张表
	_, err = markCandidateOrphans(time.Now().Add(time.Minute), nil)
	if err != nil {
		t.Fatalf("resumed scan failed: %v", err)
	}

	// 扫描到表尾后，游标应被复位为 0
	finalCur := LoadOrphanCleanCursor()
	if finalCur != 0 {
		t.Fatalf("expected cursor to reset to 0 after full scan, got %d", finalCur)
	}

	// 所有 6 条记录都应被成功打标（因为它们都不是 key_matched）
	var markedCount int64
	gdb.Model(&model.SlaveMoviePlaylist{}).Where("orphan_marked_at IS NOT NULL").Count(&markedCount)
	if markedCount != 6 {
		t.Fatalf("expected all 6 records marked as orphan candidates, got %d", markedCount)
	}
}

func TestSlaveMoviePlaylist_UnscopedDeleteTombstone(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	p := model.SlaveMoviePlaylist{
		SourceId:   "slave_role_test",
		MovieKey:   "movie_unique_1",
		GroupIndex: 0,
		GroupName:  "线路1",
		Content:    "[]",
	}
	if err := gdb.Create(&p).Error; err != nil {
		t.Fatalf("initial create failed: %v", err)
	}

	// 使用 Unscoped 硬物理删除
	if err := gdb.Unscoped().Where("source_id = ?", "slave_role_test").Delete(&model.SlaveMoviePlaylist{}).Error; err != nil {
		t.Fatalf("unscoped delete failed: %v", err)
	}

	// 再次插入相同 (source_id, movie_key, group_index)，不得发生唯一索引冲突
	p2 := model.SlaveMoviePlaylist{
		SourceId:   "slave_role_test",
		MovieKey:   "movie_unique_1",
		GroupIndex: 0,
		GroupName:  "线路1_全新",
		Content:    "[]",
	}
	if err := gdb.Create(&p2).Error; err != nil {
		t.Fatalf("re-create after unscoped delete failed with duplicate key: %v", err)
	}
}

func TestSlaveMoviePlaylist_HardDeleteNoTombstone(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	p := model.SlaveMoviePlaylist{
		SourceId:   "slave_hard_del",
		MovieKey:   "movie_unique_hd",
		GroupIndex: 0,
		GroupName:  "线路1",
		Content:    "[]",
	}
	if err := gdb.Create(&p).Error; err != nil {
		t.Fatalf("initial create failed: %v", err)
	}

	// 常规删除（无需 Unscoped）直接物理删除，杜绝 deleted_at 导致的 1062 冲突
	if err := gdb.Where("source_id = ?", "slave_hard_del").Delete(&model.SlaveMoviePlaylist{}).Error; err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	p2 := model.SlaveMoviePlaylist{
		SourceId:   "slave_hard_del",
		MovieKey:   "movie_unique_hd",
		GroupIndex: 0,
		GroupName:  "线路1_全新",
		Content:    "[]",
	}
	if err := gdb.Create(&p2).Error; err != nil {
		t.Fatalf("re-create after delete failed with duplicate key: %v", err)
	}
}


// TestCleanOrphanPlaylists_SecondChanceVerification 验证“临刑复核”机制：
// 在物理删除已超期的待定孤儿时，若发现主站 match_key 已经存在，则原地复活（清空 orphan_marked_at），不予删除。
func TestCleanOrphanPlaylists_SecondChanceVerification(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: "v1", Mid: 101, Pid: 1})
	gdb.Create(&model.FilmSource{Id: "slave_1", Name: "附属站1", Grade: model.SlaveCollect, State: true})
	// 主站当前包含 valid_resurrect_key，但不包含 real_orphan_key
	gdb.Create(&model.MovieMatchKey{Mid: 101, MatchKey: "valid_resurrect_key"})

	now := time.Now()
	expiredMark := now.Add(-50 * time.Hour) // 超过 48 小时观察期

	// 1. 待定孤儿 1：超期 50h，但其 movie_key 在主站 match_key 中已存在 -> 临刑复核必须原地复活！
	p1 := model.SlaveMoviePlaylist{
		ID:             10,
		SourceId:       "slave_1",
		MovieKey:       "valid_resurrect_key",
		GroupName:      "线路1",
		Content:        `[{"episode":"01","link":"http://a"}]`,
		OrphanMarkedAt: &expiredMark,
	}
	// 2. 待定孤儿 2：超期 50h，主站确实无此 key -> 必须被物理删除
	p2 := model.SlaveMoviePlaylist{
		ID:             20,
		SourceId:       "slave_1",
		MovieKey:       "real_orphan_key",
		GroupName:      "线路1",
		Content:        `[{"episode":"01","link":"http://b"}]`,
		OrphanMarkedAt: &expiredMark,
	}
	gdb.Create(&p1)
	gdb.Create(&p2)

	purged, err := CleanOrphanPlaylists()
	if err != nil {
		t.Fatalf("CleanOrphanPlaylists failed: %v", err)
	}
	if purged != 1 {
		t.Fatalf("expected exactly 1 real orphan purged, got %d", purged)
	}

	// 验证 p1 原地复活
	var resurrected model.SlaveMoviePlaylist
	if err := gdb.First(&resurrected, 10).Error; err != nil {
		t.Fatalf("query resurrected record failed: %v", err)
	}
	if resurrected.OrphanMarkedAt != nil {
		t.Fatalf("expected OrphanMarkedAt to be nil after second-chance revive, got %v", resurrected.OrphanMarkedAt)
	}

	// 验证 p2 被物理删除
	var dead model.SlaveMoviePlaylist
	if err := gdb.Unscoped().First(&dead, 20).Error; err == nil {
		t.Fatalf("expected real orphan 20 to be hard deleted, but found record: %+v", dead)
	}
}

// TestCleanOrphanPlaylists_ColdStartProtection 验证主站切换冷启动保护期：
// 在保护期内，孤儿清理门禁自动跳过打标与回收，防止新主站抓取期间误删存量附属资源。
func TestCleanOrphanPlaylists_ColdStartProtection(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: "v1", Mid: 101, Pid: 1})
	gdb.Create(&model.FilmSource{Id: "slave_1", Name: "附属站1", Grade: model.SlaveCollect, State: true})
	gdb.Create(&model.MovieMatchKey{Mid: 101, MatchKey: "valid_key"})

	expiredMark := time.Now().Add(-50 * time.Hour)
	p := model.SlaveMoviePlaylist{
		ID:             99,
		SourceId:       "slave_1",
		MovieKey:       "orphan_during_cold_start",
		OrphanMarkedAt: &expiredMark,
	}
	gdb.Create(&p)

	// 设置主站切换冷启动保护期
	SetMasterSwitchProtection(7 * 24 * time.Hour)
	if !InMasterSwitchProtection() {
		t.Fatal("expected InMasterSwitchProtection to be true")
	}

	// 在保护期内执行孤儿治理 -> 必须被门禁直接拦截跳过
	purged, err := CleanOrphanPlaylists()
	if err != nil {
		t.Fatalf("CleanOrphanPlaylists failed: %v", err)
	}
	if purged != 0 {
		t.Fatalf("expected 0 purged during cold start protection, got %d", purged)
	}

	// 验证记录完好无损
	var remaining model.SlaveMoviePlaylist
	if err := gdb.First(&remaining, 99).Error; err != nil {
		t.Fatalf("record 99 should remain safe during cold start: %v", err)
	}

	// 解除保护期后，再次执行治理 -> 成功物理删除真孤儿
	ClearMasterSwitchProtection()
	if InMasterSwitchProtection() {
		t.Fatal("expected InMasterSwitchProtection to be false after clear")
	}

	purged, err = CleanOrphanPlaylists()
	if err != nil {
		t.Fatalf("CleanOrphanPlaylists failed after clear: %v", err)
	}
	if purged != 1 {
		t.Fatalf("expected 1 purged after protection lifted, got %d", purged)
	}
}

// TestDisplayPlaylists_NotFilteredByOrphanMark 验证前台线路展示不得因处于观察期（orphan_marked_at != nil）而隐藏线路。
func TestDisplayPlaylists_NotFilteredByOrphanMark(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	markedTime := time.Now().Add(-1 * time.Hour)
	p := model.SlaveMoviePlaylist{
		ID:             100,
		SourceId:       "slave_1",
		MovieKey:       "key_in_observation",
		GroupIndex:     0,
		GroupName:      "高清线路",
		Content:        `[{"episode":"01","link":"http://play/1"}]`,
		OrphanMarkedAt: &markedTime,
	}
	gdb.Create(&p)

	// 1. 验证 getMultiplePlayGroupsByKeysTx 包含该线路
	groups := getMultiplePlayGroupsByKeysTx(gdb, "slave_1", "附属站1", []string{"key_in_observation"})
	if len(groups) != 1 {
		t.Fatalf("expected 1 play group returned, got %d", len(groups))
	}
	if len(groups[0].LinkList) != 1 || groups[0].LinkList[0].Link != "http://play/1" {
		t.Fatalf("unexpected play links returned: %+v", groups)
	}

	// 2. 验证 loadPlaylistsBySourceAndKeysTx 包含该线路
	bySource, err := loadPlaylistsBySourceAndKeysTx(gdb, []string{"slave_1"}, []string{"key_in_observation"})
	if err != nil {
		t.Fatalf("loadPlaylistsBySourceAndKeysTx failed: %v", err)
	}
	if len(bySource["slave_1"]["key_in_observation"]) != 1 {
		t.Fatalf("expected 1 playlist returned for key_in_observation, got %+v", bySource)
	}
}

// TestCleanOrphanPlaylists_IndependentBudgets 验证 Mark 与 Purge 各自享有独立的超时预算，且 Purge 优先于 Mark 执行。
func TestCleanOrphanPlaylists_IndependentBudgets(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: "v1", Mid: 101, Pid: 1})
	gdb.Create(&model.MovieMatchKey{Mid: 101, MatchKey: "key_matched"})

	now := time.Now()
	expiredMark := now.Add(-50 * time.Hour)

	// 插入 1 条超期待清理项，以及 1 条未打标项
	gdb.Create(&model.SlaveMoviePlaylist{
		ID:             1,
		SourceId:       "slave_1",
		MovieKey:       "expired_orphan_item",
		OrphanMarkedAt: &expiredMark,
	})
	gdb.Create(&model.SlaveMoviePlaylist{
		ID:       2,
		SourceId: "slave_1",
		MovieKey: "unmarked_orphan_item",
	})

	origPurgeBudget := orphanPlaylistPurgeBudget
	origMarkBudget := orphanPlaylistMarkBudget
	orphanPlaylistPurgeBudget = 2 * time.Second
	orphanPlaylistMarkBudget = 2 * time.Second
	defer func() {
		orphanPlaylistPurgeBudget = origPurgeBudget
		orphanPlaylistMarkBudget = origMarkBudget
	}()

	marked, purged, err := runOrphanLifecycleClean(nil)
	if err != nil {
		t.Fatalf("runOrphanLifecycleClean failed: %v", err)
	}
	if purged != 1 {
		t.Fatalf("expected 1 purged in purge phase, got %d", purged)
	}
	if marked != 1 {
		t.Fatalf("expected 1 marked in mark phase, got %d", marked)
	}

	// 验证 ID 1 已删除，ID 2 已打标
	var p1 model.SlaveMoviePlaylist
	if err := gdb.Unscoped().First(&p1, 1).Error; err == nil {
		t.Fatalf("expected p1 to be purged")
	}
	var p2 model.SlaveMoviePlaylist
	if err := gdb.First(&p2, 2).Error; err != nil || p2.OrphanMarkedAt == nil {
		t.Fatalf("expected p2 to be marked as orphan")
	}
}

// TestCleanOrphanPlaylists_PurgeTimeoutDoesNotStarveMark 验证当 Purge 预算耗尽时，Mark 依然享有独立预算正常执行，绝不饿死。
func TestCleanOrphanPlaylists_PurgeTimeoutDoesNotStarveMark(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: "v1", Mid: 101, Pid: 1})
	gdb.Create(&model.MovieMatchKey{Mid: 101, MatchKey: "key_matched"})

	now := time.Now()
	expiredMark := now.Add(-50 * time.Hour)

	// 插入 1 条超期项（ID 10），以及 1 条待打标项（ID 20）
	gdb.Create(&model.SlaveMoviePlaylist{
		ID:             10,
		SourceId:       "slave_1",
		MovieKey:       "expired_orphan_item",
		OrphanMarkedAt: &expiredMark,
	})
	gdb.Create(&model.SlaveMoviePlaylist{
		ID:       20,
		SourceId: "slave_1",
		MovieKey: "unmarked_orphan_item",
	})

	origPurgeBudget := orphanPlaylistPurgeBudget
	origMarkBudget := orphanPlaylistMarkBudget
	// 模拟 Purge 预算耗尽 (0 预算)，而 Mark 享有独立 2 秒预算
	orphanPlaylistPurgeBudget = -1 * time.Second
	orphanPlaylistMarkBudget = 2 * time.Second
	defer func() {
		orphanPlaylistPurgeBudget = origPurgeBudget
		orphanPlaylistMarkBudget = origMarkBudget
	}()

	marked, purged, err := runOrphanLifecycleClean(nil)
	if err != nil {
		t.Fatalf("runOrphanLifecycleClean failed: %v", err)
	}
	// Purge 因预算超时跳过，回收 0
	if purged != 0 {
		t.Fatalf("expected 0 purged due to purge timeout, got %d", purged)
	}
	// Mark 独立预算正常执行，成功打标 1
	if marked != 1 {
		t.Fatalf("expected 1 marked in independent mark budget, got %d", marked)
	}

	var p2 model.SlaveMoviePlaylist
	if err := gdb.First(&p2, 20).Error; err != nil || p2.OrphanMarkedAt == nil {
		t.Fatalf("expected p2 to be marked even though purge timed out")
	}
}

// TestMasterSwitchProtection_RedisIntegration 验证通过 Redis 进行主站切换冷启动保护时，
// 键删除与过期严格通过 redis.Nil authoritative 识别，防止多 Pod 进程内存脱节。
func TestMasterSwitchProtection_RedisIntegration(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	origRdb := db.Rdb
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	db.Rdb = client
	t.Cleanup(func() {
		_ = client.Close()
		db.Rdb = origRdb
		ClearMasterSwitchProtection()
	})

	// 1. 设置保护期
	SetMasterSwitchProtection(1 * time.Hour)
	if !InMasterSwitchProtection() {
		t.Fatal("expected InMasterSwitchProtection to be true with Redis")
	}

	// 2. 清除保护期（模拟在其它 Pod 执行了清除）
	ClearMasterSwitchProtection()
	// 关键验证：Redis 键已删，InMasterSwitchProtection 必须返回 false，不得因内存残留变量误判为 true
	if InMasterSwitchProtection() {
		t.Fatal("expected InMasterSwitchProtection to be false immediately after ClearMasterSwitchProtection with Redis")
	}
}

// TestOrphanCleanCursor_RedisIntegration 验证游标在 Redis 中的同步与 redis.Nil 权威识别。
func TestOrphanCleanCursor_RedisIntegration(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	origRdb := db.Rdb
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	db.Rdb = client
	t.Cleanup(func() {
		_ = client.Close()
		db.Rdb = origRdb
		ClearOrphanCleanCursor()
	})

	SaveOrphanCleanCursor(12345)
	if cur := LoadOrphanCleanCursor(); cur != 12345 {
		t.Fatalf("expected cursor 12345, got %d", cur)
	}

	ClearOrphanCleanCursor()
	if cur := LoadOrphanCleanCursor(); cur != 0 {
		t.Fatalf("expected cursor 0 after clear, got %d", cur)
	}
}

// TestSlavePlaylistMigration_DuplicateItemsAndDistributedLock 验证割接迁移中：
// 1. 脏历史数据存在相同 (source_id, movie_key, group_index) 时批次去重，杜绝 MySQL 1062 报错；
// 2. Redis 分布式排他锁在多节点竞争时，并发节点安全跳过割接。
func TestSlavePlaylistMigration_DuplicateItemsAndDistributedLock(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	origRdb := db.Rdb
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	db.Rdb = client
	t.Cleanup(func() {
		_ = client.Close()
		db.Rdb = origRdb
	})

	// 模拟老表无唯一索引或历史脏数据中存在重复键
	gdb.Exec("DROP INDEX IF EXISTS uidx_source_key_group")

	// 插入老表数据，其中包含同一 unique key 的重复项
	if err := gdb.Create(&model.MoviePlaylist{
		Model:      gorm.Model{ID: 1},
		SourceId:   "src_dup",
		MovieKey:   "key_dup",
		GroupIndex: 0,
		GroupName:  "旧版",
		Content:    `[{"episode":"01","link":"http://old"}]`,
	}).Error; err != nil {
		t.Fatalf("create dup 1: %v", err)
	}
	if err := gdb.Create(&model.MoviePlaylist{
		Model:      gorm.Model{ID: 2},
		SourceId:   "src_dup",
		MovieKey:   "key_dup",
		GroupIndex: 0,
		GroupName:  "新版",
		Content:    `[{"episode":"01","link":"http://new"}]`,
	}).Error; err != nil {
		t.Fatalf("create dup 2: %v", err)
	}

	// 执行迁移 -> 批次内自动去重，确保无 1062 异常且成功割接
	if err := MigrateLegacyMoviePlaylistsTx(gdb); err != nil {
		t.Fatalf("MigrateLegacyMoviePlaylistsTx failed with duplicates: %v", err)
	}

	var newRows []model.SlaveMoviePlaylist
	if err := gdb.Where("source_id = ?", "src_dup").Find(&newRows).Error; err != nil {
		t.Fatalf("query new rows: %v", err)
	}
	if len(newRows) != 1 {
		t.Fatalf("expected 1 deduped row in SlaveMoviePlaylist, got %d", len(newRows))
	}
	if newRows[0].GroupName != "新版" {
		t.Fatalf("expected latest version '新版', got %q", newRows[0].GroupName)
	}

	// 验证分布式排他锁行为：模拟另一节点正在割接中（持有锁）
	mr.Set(config.MigrateLegacyPlaylistsLockKey, "other-node-token")

	// 插入一条待迁移数据
	gdb.Create(&model.MoviePlaylist{
		Model:      gorm.Model{ID: 3},
		SourceId:   "src_dup",
		MovieKey:   "key_locked",
		GroupIndex: 0,
	})

	// 尝试割接 -> 竞争失败，安全跳过，返回 nil
	if err := MigrateLegacyMoviePlaylistsTx(gdb); err != nil {
		t.Fatalf("expected nil when lock held by other node, got %v", err)
	}

	// 释放分布式锁后，再次割接 -> 成功迁移该记录
	mr.Del(config.MigrateLegacyPlaylistsLockKey)
	if err := MigrateLegacyMoviePlaylistsTx(gdb); err != nil {
		t.Fatalf("MigrateLegacyMoviePlaylistsTx failed after lock released: %v", err)
	}
}

func TestInMasterSwitchProtection_FailSafe(t *testing.T) {
	origRdb := db.Rdb
	defer func() {
		db.Rdb = origRdb
		ClearMasterSwitchProtection()
	}()

	// 1. Redis 为 nil，内存无保护 -> 返回 false
	db.Rdb = nil
	ClearMasterSwitchProtection()
	if InMasterSwitchProtection() {
		t.Fatalf("expected false when db.Rdb is nil and memory is cleared")
	}

	// 2. 正常 Redis 存在 key 未过期 -> 返回 true
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	db.Rdb = client

	SetMasterSwitchProtection(time.Hour)
	if !InMasterSwitchProtection() {
		t.Fatalf("expected true when protected key exists in Redis")
	}

	// 3. 正常 Redis key 不存在 (redis.Nil) -> 返回 false
	ClearMasterSwitchProtection()
	if InMasterSwitchProtection() {
		t.Fatalf("expected false when key does not exist (redis.Nil)")
	}

	// 4. Redis 物理宕机/网络故障 (非 redis.Nil 错误) -> Fail-Safe 保守安全返回 true
	mr.Close() // 强制关闭后端，模拟 Redis 宕机
	if !InMasterSwitchProtection() {
		t.Fatalf("expected true under Fail-Safe mode when Redis query returns network error")
	}
}

func TestPurgeExpiredOrphans_TOCTOUGuard(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	// 快照与匹配键
	gdb.Create(&model.FilmListSnapshot{SnapshotVersion: "v1", Mid: 201, Pid: 1})
	gdb.Create(&model.FilmSource{Id: "slave_toctou", Name: "附属站TOCTOU", Grade: model.SlaveCollect, State: true})
	gdb.Create(&model.MovieMatchKey{Mid: 201, MatchKey: "valid_key"})

	// 插入超期孤儿：已标记超过 48 小时
	past := time.Now().Add(-50 * time.Hour)
	orphan1 := model.SlaveMoviePlaylist{
		ID:             1001,
		SourceId:       "slave_toctou",
		MovieKey:       "key_to_be_revived",
		OrphanMarkedAt: &past,
	}
	orphan2 := model.SlaveMoviePlaylist{
		ID:             1002,
		SourceId:       "slave_toctou",
		MovieKey:       "key_true_orphan",
		OrphanMarkedAt: &past,
	}
	gdb.Create(&orphan1)
	gdb.Create(&orphan2)

	// 模拟在临刑复核与物理删除之间的极窄毫秒窗口内，主站爬虫抓取完成并将 1001 复活（orphan_marked_at 置为 NULL）
	gdb.Model(&model.SlaveMoviePlaylist{}).Where("id = ?", 1001).Update("orphan_marked_at", nil)

	// 调用 purgeExpiredOrphans 执行清理流程
	purged, err := purgeExpiredOrphans(time.Now().Add(time.Minute), nil)
	if err != nil {
		t.Fatalf("purgeExpiredOrphans failed: %v", err)
	}

	// 1002 真实超期被物理删除，1001 因原子守卫 orphan_marked_at IS NOT NULL 而得以保全
	if purged != 1 {
		t.Fatalf("expected 1 purged row (only 1002), got %d", purged)
	}

	// 验证 1001 依然安好存在于库中
	var count1001 int64
	gdb.Model(&model.SlaveMoviePlaylist{}).Where("id = ?", 1001).Count(&count1001)
	if count1001 != 1 {
		t.Fatalf("expected revived playlist 1001 to survive, count=%d", count1001)
	}

	// 验证 1002 已被物理删除
	var count1002 int64
	gdb.Model(&model.SlaveMoviePlaylist{}).Where("id = ?", 1002).Count(&count1002)
	if count1002 != 0 {
		t.Fatalf("expected expired orphan 1002 to be deleted, count=%d", count1002)
	}
}

func TestMigrateLegacyMoviePlaylistsTx_LargeBatchChunking(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	// 插入 1,200 条老表数据（突破单批 500 条分块限制）
	const totalItems = 1200
	legacyItems := make([]model.MoviePlaylist, 0, totalItems)
	for i := 1; i <= totalItems; i++ {
		legacyItems = append(legacyItems, model.MoviePlaylist{
			Model:      gorm.Model{ID: uint(i)},
			SourceId:   fmt.Sprintf("source_%d", i%10),
			MovieKey:   fmt.Sprintf("key_%d", i),
			GroupIndex: 0,
			GroupName:  "默认线路",
			Content:    "第01集$http://example.com/play/1.m3u8",
		})
	}

	// 在 SQLite 中分块插入 1200 条
	for i := 0; i < len(legacyItems); i += 400 {
		end := i + 400
		if end > len(legacyItems) {
			end = len(legacyItems)
		}
		if err := gdb.Create(legacyItems[i:end]).Error; err != nil {
			t.Fatalf("seed legacy items failed: %v", err)
		}
	}

	var legacyBefore int64
	gdb.Model(&model.MoviePlaylist{}).Count(&legacyBefore)
	if legacyBefore != totalItems {
		t.Fatalf("expected %d legacy items before migration, got %d", totalItems, legacyBefore)
	}

	// 执行割接迁移
	if err := MigrateLegacyMoviePlaylistsTx(gdb); err != nil {
		t.Fatalf("MigrateLegacyMoviePlaylistsTx failed: %v", err)
	}

	// 验证老表全部清空
	var legacyAfter int64
	gdb.Model(&model.MoviePlaylist{}).Count(&legacyAfter)
	if legacyAfter != 0 {
		t.Fatalf("expected 0 legacy items after migration, got %d", legacyAfter)
	}

	// 验证新表完整包含 1,200 条记录
	var slaveCount int64
	gdb.Model(&model.SlaveMoviePlaylist{}).Count(&slaveCount)
	if slaveCount != totalItems {
		t.Fatalf("expected %d slave movie playlists after migration, got %d", totalItems, slaveCount)
	}
}

func TestRefreshAfterDataClean(t *testing.T) {
	gdb := setupOrphanCleanerTestDB(t)

	// 插入影片与详情
	detailJSON, _ := json.Marshal(model.MovieDetail{
		Id:       501,
		PlayFrom: []string{"qq"},
		PlayList: [][]model.MovieUrlInfo{
			{{Episode: "01", Link: "https://v.qq.com/x/cover/1.html"}},
		},
	})
	detail := model.MovieDetailInfo{
		Mid:     501,
		Content: string(detailJSON),
	}
	if err := gdb.Create(&detail).Error; err != nil {
		t.Fatalf("create detail: %v", err)
	}

	index := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid: 501,
		},
		FilmIndexCategory: model.FilmIndexCategory{
			Cid: 1,
			Pid: 1,
		},
		FilmIndexDerived: model.FilmIndexDerived{
			PlayFromSummary: "", // 缺失播放源摘要
		},
	}
	if err := gdb.Create(&index).Error; err != nil {
		t.Fatalf("create index: %v", err)
	}

	if err := RefreshAfterDataClean(); err != nil {
		t.Fatalf("RefreshAfterDataClean failed: %v", err)
	}

	// 验证快照已发布
	hasSnapshot, err := HasPublishedFilmListSnapshot()
	if err != nil {
		t.Fatalf("HasPublishedFilmListSnapshot failed: %v", err)
	}
	if !hasSnapshot {
		t.Fatal("expected snapshot to be published after RefreshAfterDataClean")
	}

	// 验证缺失的 PlayFromSummary 已被补齐
	var updatedIndex model.FilmIndex
	if err := gdb.Where("mid = ?", 501).First(&updatedIndex).Error; err != nil {
		t.Fatalf("query updated index: %v", err)
	}
	if updatedIndex.PlayFromSummary == "" {
		t.Fatal("expected PlayFromSummary to be refreshed")
	}
}

