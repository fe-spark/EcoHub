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
	masterSwitchProtectUntil time.Time
	masterSwitchMu           sync.RWMutex
)

// MasterSwitchColdStartDuration 主站切换冷启动保护期默认时长：7 天
const MasterSwitchColdStartDuration = 7 * 24 * time.Hour

func setMasterSwitchProtectUntil(until time.Time) {
	masterSwitchMu.Lock()
	masterSwitchProtectUntil = until
	masterSwitchMu.Unlock()
}

// SetMasterSwitchProtection 设置主站切换冷启动保护期。运行时只认内存；Redis 仅作进程重启备忘（TTL 与保护期一致，默认 7 天）。
func SetMasterSwitchProtection(duration time.Duration) {
	if duration <= 0 {
		duration = MasterSwitchColdStartDuration
	}
	until := time.Now().Add(duration)
	if db.Rdb != nil {
		if err := db.Rdb.Set(db.Cxt, config.MasterSwitchProtectKey, until.Unix(), duration).Err(); err != nil {
			log.Printf("[SetMasterSwitchProtection] 写入 Redis 保护期失败: %v", err)
		}
	}
	setMasterSwitchProtectUntil(until)
}

// RestoreMasterSwitchProtection 启动时从 Redis 读一次填回内存。之后不再查 Redis。
func RestoreMasterSwitchProtection() {
	if db.Rdb == nil {
		return
	}
	val, err := db.Rdb.Get(db.Cxt, config.MasterSwitchProtectKey).Int64()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return
		}
		log.Printf("[RestoreMasterSwitchProtection] 从 Redis 恢复保护期失败，启用默认 %s 保护: %v", MasterSwitchColdStartDuration, err)
		setMasterSwitchProtectUntil(time.Now().Add(MasterSwitchColdStartDuration))
		return
	}
	until := time.Unix(val, 0)
	if !until.After(time.Now()) {
		return
	}
	setMasterSwitchProtectUntil(until)
	log.Printf("[RestoreMasterSwitchProtection] 已从 Redis 恢复主站切换保护期 until=%s", until.Format(time.RFC3339))
}

// InMasterSwitchProtection 判断是否处于主站切换冷启动保护期（只读内存）。
func InMasterSwitchProtection() bool {
	masterSwitchMu.RLock()
	defer masterSwitchMu.RUnlock()
	return time.Now().Before(masterSwitchProtectUntil)
}

// ClearMasterSwitchProtection 清除内存保护期，并删掉 Redis 备忘。
func ClearMasterSwitchProtection() {
	if db.Rdb != nil {
		if err := db.Rdb.Del(db.Cxt, config.MasterSwitchProtectKey).Err(); err != nil {
			log.Printf("[ClearMasterSwitchProtection] 删除 Redis 保护期失败: %v", err)
		}
	}
	setMasterSwitchProtectUntil(time.Time{})
}

var (
	memoryOrphanCursor uint
	memoryOrphanMu     sync.Mutex
)

// LoadOrphanCleanCursor 读取孤儿治理断点游标（只认内存）。
func LoadOrphanCleanCursor() uint {
	memoryOrphanMu.Lock()
	defer memoryOrphanMu.Unlock()
	return memoryOrphanCursor
}

// SaveOrphanCleanCursor 保存断点游标。运行时只认内存；Redis 仅作进程重启备忘。
func SaveOrphanCleanCursor(id uint) {
	if db.Rdb != nil {
		if err := db.Rdb.Set(db.Cxt, config.OrphanCleanCursorKey, id, 0).Err(); err != nil {
			log.Printf("[SaveOrphanCleanCursor] 写入 Redis 失败: %v", err)
		}
	}
	memoryOrphanMu.Lock()
	memoryOrphanCursor = id
	memoryOrphanMu.Unlock()
}

// RestoreOrphanCleanCursor 启动时从 Redis 读一次填回内存。
func RestoreOrphanCleanCursor() {
	if db.Rdb == nil {
		return
	}
	val, err := db.Rdb.Get(db.Cxt, config.OrphanCleanCursorKey).Uint64()
	if err != nil {
		return
	}
	memoryOrphanMu.Lock()
	memoryOrphanCursor = uint(val)
	memoryOrphanMu.Unlock()
}

// ClearOrphanCleanCursor 清除内存游标，并删掉 Redis 备忘。
func ClearOrphanCleanCursor() {
	if db.Rdb != nil {
		_ = db.Rdb.Del(db.Cxt, config.OrphanCleanCursorKey).Err()
	}
	memoryOrphanMu.Lock()
	memoryOrphanCursor = 0
	memoryOrphanMu.Unlock()
}

var (
	orphanPlaylistScanBatchSize   = 500
	orphanPlaylistDeleteBatchSize = 100
	orphanPlaylistBatchCooldown   = 15 * time.Millisecond
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

// CleanOrphanPlaylists 极简单阶段分批物理回收附属站真孤儿。
func CleanOrphanPlaylists() (int64, error) {
	return CleanOrphanPlaylistsUntil(nil)
}

// CleanOrphanPlaylistsUntil 24 小时安全沉淀期 + 普通分批 SQL 清理。
// 仅清理 created_at < NOW() - 24h 的孤儿记录，24h 内新建记录天然跳过防误删。
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
			ClearOrphanCleanCursor()
			break
		}

		lastID = rows[len(rows)-1].ID

		seenKeys := make(map[string]struct{}, len(rows))
		candidateKeys := make([]string, 0, len(rows))
		candidateRows := make([]playlistScanRow, 0, len(rows))
		for _, r := range rows {
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
