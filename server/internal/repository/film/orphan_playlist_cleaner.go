package film

import (
	"errors"
	"log"
	"sort"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

var (
	memoryOrphanCursor uint
	memoryOrphanMu     sync.Mutex

	memoryMasterProtectUntil time.Time
	memoryMasterProtectMu    sync.RWMutex
)

// MasterSwitchColdStartDuration 主站切换冷启动保护期默认时长：7 天
const MasterSwitchColdStartDuration = 7 * 24 * time.Hour

// SetMasterSwitchProtection 设置主站切换冷启动保护期
func SetMasterSwitchProtection(duration time.Duration) {
	if duration <= 0 {
		duration = MasterSwitchColdStartDuration
	}
	until := time.Now().Add(duration)
	if db.Rdb != nil {
		_ = db.Rdb.Set(db.Cxt, config.MasterSwitchProtectKey, until.Unix(), duration).Err()
	}
	memoryMasterProtectMu.Lock()
	memoryMasterProtectUntil = until
	memoryMasterProtectMu.Unlock()
}

// InMasterSwitchProtection 判断是否处于主站切换冷启动保护期
func InMasterSwitchProtection() bool {
	if db.Rdb != nil {
		val, err := db.Rdb.Get(db.Cxt, config.MasterSwitchProtectKey).Int64()
		if err == nil {
			return val > time.Now().Unix()
		}
		if errors.Is(err, redis.Nil) {
			return false
		}
		log.Printf("[InMasterSwitchProtection] 查询 Redis 保护期异常，启用 Fail-Safe 保守安全策略: %v", err)
		return true
	}
	memoryMasterProtectMu.RLock()
	defer memoryMasterProtectMu.RUnlock()
	return time.Now().Before(memoryMasterProtectUntil)
}

// ClearMasterSwitchProtection 清除主站切换冷启动保护期（供测试重置或手动提前解除）
func ClearMasterSwitchProtection() {
	if db.Rdb != nil {
		_ = db.Rdb.Del(db.Cxt, config.MasterSwitchProtectKey).Err()
	}
	memoryMasterProtectMu.Lock()
	memoryMasterProtectUntil = time.Time{}
	memoryMasterProtectMu.Unlock()
}

// LoadOrphanCleanCursor 获取附属站孤儿治理断点游标（优先读取 Redis，降级使用内存）
func LoadOrphanCleanCursor() uint {
	if db.Rdb != nil {
		val, err := db.Rdb.Get(db.Cxt, config.OrphanCleanCursorKey).Uint64()
		if err == nil {
			return uint(val)
		}
		if errors.Is(err, redis.Nil) {
			return 0
		}
		// Redis 异常时降级使用本机内存
	}
	memoryOrphanMu.Lock()
	defer memoryOrphanMu.Unlock()
	return memoryOrphanCursor
}

// SaveOrphanCleanCursor 保存附属站孤儿治理断点游标
func SaveOrphanCleanCursor(id uint) {
	if db.Rdb != nil {
		_ = db.Rdb.Set(db.Cxt, config.OrphanCleanCursorKey, id, 7*24*time.Hour).Err()
	}
	memoryOrphanMu.Lock()
	memoryOrphanCursor = id
	memoryOrphanMu.Unlock()
}

// ClearOrphanCleanCursor 清除附属站孤儿治理游标（全量轮次结束或数据重置时复位）
func ClearOrphanCleanCursor() {
	if db.Rdb != nil {
		_ = db.Rdb.Del(db.Cxt, config.OrphanCleanCursorKey).Err()
	}
	memoryOrphanMu.Lock()
	memoryOrphanCursor = 0
	memoryOrphanMu.Unlock()
}

const orphanPlaylistDeleteBatchSize = 200
const inMatchKeyBatchSize = 500

var (
	orphanPlaylistScanBatchSize = 5000
	orphanPlaylistBatchCooldown = 50 * time.Millisecond
	orphanPlaylistPurgeBudget   = 1 * time.Minute
	orphanPlaylistMarkBudget    = 1 * time.Minute
	orphanPlaylistRunBudget     = 2 * time.Minute
	orphanPlaylistGracePeriod   = 48 * time.Hour
)

type orphanPlaylistRow struct {
	ID uint
}

type playlistScanRow struct {
	ID             uint
	MovieKey       string
	SourceId       string
	OrphanMarkedAt *time.Time
}

type matchKeyRow struct {
	MatchKey string
}

// CleanOrphanPlaylists 两阶段状态机清理附属站孤儿：
// 1. Mark: 未见主站 match_key 的行打上 orphan_marked_at（进入 48h 观察期）；
// 2. Purge: 已被标记且超期 48h 的真孤儿执行物理回收。
func CleanOrphanPlaylists() (int64, error) {
	return CleanOrphanPlaylistsUntil(nil)
}

// CleanOrphanPlaylistsUntil 兼容旧签名接口，内部执行状态机标记与超期回收。
func CleanOrphanPlaylistsUntil(shouldStop func() bool) (int64, error) {
	marked, purged, err := runOrphanLifecycleClean(shouldStop)
	if err != nil {
		return purged, err
	}
	if marked > 0 || purged > 0 {
		log.Printf("[CleanOrphan] 孤儿治理完成: 新增待定标记=%d, 物理回收真孤儿=%d", marked, purged)
	}
	return purged, nil
}

func runOrphanLifecycleClean(shouldStop func() bool) (int64, int64, error) {
	if db.Mdb == nil {
		return 0, 0, nil
	}
	if InMasterSwitchProtection() {
		log.Println("[CleanOrphan] 处于主站切换冷启动保护期，跳过孤儿标记与清理")
		return 0, 0, nil
	}
	if hasSnapshot, err := HasPublishedFilmListSnapshot(); err != nil {
		return 0, 0, err
	} else if !hasSnapshot {
		log.Println("[CleanOrphan] 主站快照未发布，跳过孤儿标记与清理")
		return 0, 0, nil
	}
	if hasKeys, err := hasMovieMatchKeys(); err != nil {
		return 0, 0, err
	} else if !hasKeys {
		log.Println("[CleanOrphan] movie_match_key 为空，跳过孤儿标记与清理")
		return 0, 0, nil
	}

	// 第一阶段：物理回收超期 48 小时真孤儿 (Purge Phase，优先执行，享有独立 1 分钟预算)
	purgeDeadline := time.Now().Add(orphanPlaylistPurgeBudget)
	purged, err := purgeExpiredOrphans(purgeDeadline, shouldStop)
	if err != nil {
		return 0, purged, err
	}

	// 第二阶段：标记待定孤儿 (Mark Phase，享有独立 1 分钟预算)
	markDeadline := time.Now().Add(orphanPlaylistMarkBudget)
	marked, err := markCandidateOrphans(markDeadline, shouldStop)
	if err != nil {
		return marked, purged, err
	}

	return marked, purged, nil
}

// markCandidateOrphans 扫描未打标的附属站记录，未匹配主站 match_key 者打上当前时间戳。
// 引入 Redis/内存游标断点续扫，避免大表在运行预算超时退出后下次永远从头扫描造成饥饿。
func markCandidateOrphans(deadline time.Time, shouldStop func() bool) (int64, error) {
	if db.Mdb == nil {
		return 0, nil
	}
	var markedTotal int64
	lastID := LoadOrphanCleanCursor()

	for {
		if shouldStop != nil && shouldStop() {
			SaveOrphanCleanCursor(lastID)
			break
		}
		if time.Now().After(deadline) {
			SaveOrphanCleanCursor(lastID)
			break
		}

		var rows []playlistScanRow
		err := db.Mdb.Model(&model.SlaveMoviePlaylist{}).
			Select("id, movie_key, source_id").
			Where("id > ? AND orphan_marked_at IS NULL", lastID).
			Order("id ASC").
			Limit(orphanPlaylistScanBatchSize).
			Scan(&rows).Error
		if err != nil {
			SaveOrphanCleanCursor(lastID)
			return markedTotal, err
		}
		if len(rows) == 0 {
			// 当前轮次扫描完成，游标归零
			ClearOrphanCleanCursor()
			break
		}

		lastID = rows[len(rows)-1].ID

		seenKeys := make(map[string]struct{}, len(rows))
		keys := make([]string, 0, len(rows))
		for _, r := range rows {
			if r.MovieKey == "" {
				continue
			}
			if _, ok := seenKeys[r.MovieKey]; !ok {
				seenKeys[r.MovieKey] = struct{}{}
				keys = append(keys, r.MovieKey)
			}
		}

		existingKeys, err := loadExistingMatchKeySet(keys)
		if err != nil {
			SaveOrphanCleanCursor(lastID)
			return markedTotal, err
		}

		orphanIDs := make([]uint, 0, len(rows))
		for _, r := range rows {
			if r.MovieKey == "" {
				orphanIDs = append(orphanIDs, r.ID)
				continue
			}
			if _, ok := existingKeys[r.MovieKey]; !ok {
				orphanIDs = append(orphanIDs, r.ID)
			}
		}

		if len(orphanIDs) > 0 {
			now := time.Now()
			for i := 0; i < len(orphanIDs); i += orphanPlaylistDeleteBatchSize {
				end := i + orphanPlaylistDeleteBatchSize
				if end > len(orphanIDs) {
					end = len(orphanIDs)
				}
				sub := orphanIDs[i:end]
				if err := db.Mdb.Model(&model.SlaveMoviePlaylist{}).
					Where("id IN ? AND orphan_marked_at IS NULL", sub).
					Update("orphan_marked_at", now).Error; err != nil {
					SaveOrphanCleanCursor(lastID)
					return markedTotal, err
				}
				markedTotal += int64(len(sub))
			}
		}

		SaveOrphanCleanCursor(lastID)
		time.Sleep(orphanPlaylistBatchCooldown)
	}

	return markedTotal, nil
}

// purgeExpiredOrphans 物理删除已标记且超出 orphanPlaylistGracePeriod 的真孤儿。
// 在物理删除 candidate IDs 之前，先获取其对应的 movie_key 并分批调用 loadExistingMatchKeySet 执行“临刑复核”：
// 若发现当前主站已存在该 movie_key，立即原地复活（orphan_marked_at = NULL），严禁删除；
// 只有确认主站依然不存在的记录才执行 Unscoped().Delete()。物理删除前对 IDs 升序排序以规避死锁。
func purgeExpiredOrphans(deadline time.Time, shouldStop func() bool) (int64, error) {
	if db.Mdb == nil {
		return 0, nil
	}
	var purgedTotal int64
	cutoff := time.Now().Add(-orphanPlaylistGracePeriod)

	for {
		if shouldStop != nil && shouldStop() {
			break
		}
		if time.Now().After(deadline) {
			break
		}

		type orphanCandidateRow struct {
			ID       uint   `gorm:"column:id"`
			MovieKey string `gorm:"column:movie_key"`
		}
		var candidates []orphanCandidateRow
		err := db.Mdb.Model(&model.SlaveMoviePlaylist{}).
			Select("id, movie_key").
			Where("orphan_marked_at IS NOT NULL AND orphan_marked_at < ?", cutoff).
			Limit(orphanPlaylistDeleteBatchSize).
			Scan(&candidates).Error
		if err != nil {
			return purgedTotal, err
		}
		if len(candidates) == 0 {
			break
		}

		candidateKeys := make([]string, 0, len(candidates))
		seenKeys := make(map[string]struct{}, len(candidates))
		for _, c := range candidates {
			if c.MovieKey != "" {
				if _, ok := seenKeys[c.MovieKey]; !ok {
					seenKeys[c.MovieKey] = struct{}{}
					candidateKeys = append(candidateKeys, c.MovieKey)
				}
			}
		}

		existingKeys, err := loadExistingMatchKeySet(candidateKeys)
		if err != nil {
			return purgedTotal, err
		}

		var reviveIDs []uint
		var deleteIDs []uint
		for _, c := range candidates {
			if c.MovieKey != "" {
				if _, ok := existingKeys[c.MovieKey]; ok {
					reviveIDs = append(reviveIDs, c.ID)
					continue
				}
			}
			deleteIDs = append(deleteIDs, c.ID)
		}

		if len(reviveIDs) > 0 {
			sort.Slice(reviveIDs, func(i, j int) bool { return reviveIDs[i] < reviveIDs[j] })
			if err := db.Mdb.Model(&model.SlaveMoviePlaylist{}).
				Where("id IN ?", reviveIDs).
				Update("orphan_marked_at", gorm.Expr("NULL")).Error; err != nil {
				return purgedTotal, err
			}
		}

		if len(deleteIDs) > 0 {
			sort.Slice(deleteIDs, func(i, j int) bool { return deleteIDs[i] < deleteIDs[j] })
			res := db.Mdb.Unscoped().Where("id IN ? AND orphan_marked_at IS NOT NULL AND orphan_marked_at < ?", deleteIDs, cutoff).Delete(&model.SlaveMoviePlaylist{})
			if res.Error != nil {
				return purgedTotal, res.Error
			}
			purgedTotal += res.RowsAffected
		}

		if len(candidates) < orphanPlaylistDeleteBatchSize {
			break
		}
		time.Sleep(orphanPlaylistBatchCooldown)
	}

	return purgedTotal, nil
}

func HasPublishedFilmListSnapshot() (bool, error) {
	if db.Mdb == nil {
		return false, nil
	}
	var row orphanPlaylistRow
	if err := db.Mdb.Model(&model.FilmListSnapshot{}).Select("id").Limit(1).Scan(&row).Error; err != nil {
		return false, err
	}
	return row.ID > 0, nil
}

func hasMovieMatchKeys() (bool, error) {
	if db.Mdb == nil {
		return false, nil
	}
	var row orphanPlaylistRow
	if err := db.Mdb.Model(&model.MovieMatchKey{}).Select("id").Limit(1).Scan(&row).Error; err != nil {
		return false, err
	}
	return row.ID > 0, nil
}

// loadExistingMatchKeySet 分块检索匹配键，规避 SQLite 参数超限 (too many SQL variables) 与慢查询
func loadExistingMatchKeySet(keys []string) (map[string]struct{}, error) {
	existing := make(map[string]struct{}, len(keys))
	if len(keys) == 0 || db.Mdb == nil {
		return existing, nil
	}
	for i := 0; i < len(keys); i += inMatchKeyBatchSize {
		end := i + inMatchKeyBatchSize
		if end > len(keys) {
			end = len(keys)
		}
		chunk := keys[i:end]
		var rows []matchKeyRow
		if err := db.Mdb.Model(&model.MovieMatchKey{}).
			Select("match_key").
			Where("match_key IN ?", chunk).
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			existing[row.MatchKey] = struct{}{}
		}
	}
	return existing, nil
}

func RefreshAfterDataClean() error {
	if db.Mdb == nil {
		return nil
	}
	refreshMissingPlayFromSummaries()
	return ActivateRebuiltFilmListSnapshot(NewSnapshotVersion())
}
