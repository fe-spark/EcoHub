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

var (
	orphanPlaylistScanBatchSize   = 1000
	orphanPlaylistDeleteBatchSize = 100
	orphanPlaylistBatchCooldown   = 20 * time.Millisecond
	orphanPlaylistMaxPurgePerRun  = int64(5000)
	orphanPlaylistMaxRunDuration  = 10 * time.Second
	orphanPlaylistGracePeriod     = 24 * time.Hour
)

const inMatchKeyBatchSize = 500

type orphanPlaylistRow struct {
	ID uint
}

type playlistScanRow struct {
	ID        uint
	MovieKey  string
	SourceId  string
	CreatedAt time.Time
}

type matchKeyRow struct {
	MatchKey string
}

// CleanOrphanPlaylists 极简单阶段微批物理回收附属站真孤儿。
func CleanOrphanPlaylists() (int64, error) {
	return CleanOrphanPlaylistsUntil(nil)
}

// CleanOrphanPlaylistsUntil Keyset 分页断点续扫 + 24 小时安全沉淀期 + 微批直接物理清理。
// 彻底废除两阶段状态机与 19 万次打标写放大，遵守工业级高可用四大铁律：
// 1. Keyset Pagination：WHERE id > lastID ORDER BY id ASC LIMIT 1000 恒定走主键聚集索引；
// 2. 24 小时安全沉淀：仅 created_at < NOW() - 24h 的记录进入判定，24h 内新建记录天然跳过防误删；
// 3. 安全配额限流：单次最大删除 5000 条或耗时超限即停，未完成保留断点游标，杜绝集中到期拖垮数据库；
// 4. 微批微休眠：每次删除最多 100 条并按 ID 升序排序防死锁，批间休眠 20ms 防 I/O 尖刺。
func CleanOrphanPlaylistsUntil(shouldStop func() bool) (int64, error) {
	if db.Mdb == nil {
		return 0, nil
	}
	if InMasterSwitchProtection() {
		log.Println("[CleanOrphan] 处于主站切换冷启动保护期，跳过孤儿清理")
		return 0, nil
	}
	if hasSnapshot, err := HasPublishedFilmListSnapshot(); err != nil {
		return 0, err
	} else if !hasSnapshot {
		log.Println("[CleanOrphan] 主站快照未发布，跳过孤儿清理")
		return 0, nil
	}
	if hasKeys, err := hasMovieMatchKeys(); err != nil {
		return 0, err
	} else if !hasKeys {
		log.Println("[CleanOrphan] movie_match_key 为空，跳过孤儿清理")
		return 0, nil
	}

	startedAt := time.Now()
	deadline := startedAt.Add(orphanPlaylistMaxRunDuration)
	cutoff := startedAt.Add(-orphanPlaylistGracePeriod)
	var purgedTotal int64
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
		if purgedTotal >= orphanPlaylistMaxPurgePerRun {
			SaveOrphanCleanCursor(lastID)
			break
		}

		var rows []playlistScanRow
		err := db.Mdb.Model(&model.SlaveMoviePlaylist{}).
			Select("id, movie_key, source_id, created_at").
			Where("id > ?", lastID).
			Order("id ASC").
			Limit(orphanPlaylistScanBatchSize).
			Scan(&rows).Error
		if err != nil {
			SaveOrphanCleanCursor(lastID)
			return purgedTotal, err
		}
		if len(rows) == 0 {
			// 当前轮次扫描完成，游标归零
			ClearOrphanCleanCursor()
			break
		}

		lastID = rows[len(rows)-1].ID

		// 筛选已满足 24 小时安全沉淀期的候选记录
		seenKeys := make(map[string]struct{}, len(rows))
		candidateKeys := make([]string, 0, len(rows))
		candidateRows := make([]playlistScanRow, 0, len(rows))

		for _, r := range rows {
			// 未满 24 小时的记录天然受保护跳过，主站可能有采集延迟
			if !r.CreatedAt.Before(cutoff) {
				continue
			}
			candidateRows = append(candidateRows, r)
			if r.MovieKey != "" {
				if _, ok := seenKeys[r.MovieKey]; !ok {
					seenKeys[r.MovieKey] = struct{}{}
					candidateKeys = append(candidateKeys, r.MovieKey)
				}
			}
		}

		if len(candidateRows) > 0 {
			existingKeys, err := loadExistingMatchKeySet(candidateKeys)
			if err != nil {
				SaveOrphanCleanCursor(lastID)
				return purgedTotal, err
			}

			orphanIDs := make([]uint, 0, len(candidateRows))
			for _, r := range candidateRows {
				if r.MovieKey == "" {
					orphanIDs = append(orphanIDs, r.ID)
					continue
				}
				if _, ok := existingKeys[r.MovieKey]; !ok {
					orphanIDs = append(orphanIDs, r.ID)
				}
			}

			if len(orphanIDs) > 0 {
				sort.Slice(orphanIDs, func(i, j int) bool { return orphanIDs[i] < orphanIDs[j] })
				for i := 0; i < len(orphanIDs); i += orphanPlaylistDeleteBatchSize {
					end := i + orphanPlaylistDeleteBatchSize
					if end > len(orphanIDs) {
						end = len(orphanIDs)
					}
					sub := orphanIDs[i:end]
					res := db.Mdb.Unscoped().
						Where("id IN ? AND created_at < ?", sub, cutoff).
						Delete(&model.SlaveMoviePlaylist{})
					if res.Error != nil {
						SaveOrphanCleanCursor(lastID)
						return purgedTotal, res.Error
					}
					purgedTotal += res.RowsAffected
					if purgedTotal >= orphanPlaylistMaxPurgePerRun {
						break
					}
				}
			}
		}

		SaveOrphanCleanCursor(lastID)
		if orphanPlaylistBatchCooldown > 0 {
			time.Sleep(orphanPlaylistBatchCooldown)
		}
	}

	if purgedTotal > 0 {
		log.Printf("[CleanOrphan] 孤儿治理完成: 物理回收真孤儿=%d, cost=%s", purgedTotal, time.Since(startedAt))
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
