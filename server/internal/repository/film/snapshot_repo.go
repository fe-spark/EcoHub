package film

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm/clause"
)

const (
	snapshotBuildBatchSize = 1000
	snapshotRetainVersions = 2
)

var (
	activeSnapshotUpsertMu sync.Mutex
	activeSnapshotMu       sync.Mutex
	activeSnapshotVersion  string
)

func GetActiveSnapshotVersion() string {
	activeSnapshotMu.Lock()
	defer activeSnapshotMu.Unlock()
	return strings.TrimSpace(activeSnapshotVersion)
}

func SetActiveSnapshotVersion(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil
	}
	activeSnapshotMu.Lock()
	activeSnapshotVersion = version
	activeSnapshotMu.Unlock()
	if db.Rdb != nil {
		if err := db.Rdb.Set(db.Cxt, config.SnapshotActiveVersionKey, version, 0).Err(); err != nil {
			log.Printf("[SetActiveSnapshotVersion] 写入 Redis 备忘失败: %v", err)
		}
	}
	return nil
}

func clearActiveSnapshotVersion() {
	activeSnapshotMu.Lock()
	activeSnapshotVersion = ""
	activeSnapshotMu.Unlock()
}

// RestoreActiveSnapshotVersion 启动时从 Redis 读一次填回内存。
func RestoreActiveSnapshotVersion() {
	if db.Rdb == nil {
		return
	}
	version, err := db.Rdb.Get(db.Cxt, config.SnapshotActiveVersionKey).Result()
	version = strings.TrimSpace(version)
	if err != nil || version == "" {
		return
	}
	activeSnapshotMu.Lock()
	activeSnapshotVersion = version
	activeSnapshotMu.Unlock()
}

func ResetActiveSnapshotFallbackForTest() {
	clearActiveSnapshotVersion()
}

func GetActiveReadModelVersion() string {
	snapshotVer := GetActiveSnapshotVersion()
	rm := GetActiveFilmReadModel()
	rmVersion := ""
	if rm != nil {
		rmVersion = rm.Version
	}
	return resolveActiveReadModelVersion(rmVersion, snapshotVer)
}

// resolveActiveReadModelVersion 决定对外公布的读模型版本。
func resolveActiveReadModelVersion(rmVersion, snapshotVer string) string {
	if snapshotVer != "" {
		return snapshotVer
	}
	return rmVersion
}

// activeReadModelVersion 取内存读模型版本；空指针或 Version 为空时回退到活跃快照版本。
// ClearActiveFilmReadModel / init 都会写入 Version="" 的非空指针，不能把非空指针当成有效版本。
func activeReadModelVersion(readModel *FilmReadModel, snapshotVersion string) string {
	if readModel != nil {
		if version := strings.TrimSpace(readModel.Version); version != "" {
			return version
		}
	}
	return snapshotVersion
}

func NewSnapshotVersion() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func RebuildFilmListSnapshot(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		version = NewSnapshotVersion()
	}

	startedAt := time.Now()
	if err := db.Mdb.Where("snapshot_version = ?", version).Unscoped().Delete(&model.FilmListSnapshot{}).Error; err != nil {
		return err
	}

	var lastID uint
	total := 0
	for {
		batchStartedAt := time.Now()
		var indexes []model.FilmIndex
		if err := db.Mdb.Joins("JOIN "+model.TableMovieDetail+" ON "+model.TableMovieDetail+".mid = film_index.mid AND "+model.TableMovieDetail+".deleted_at IS NULL").
			Where("film_index.id > ?", lastID).
			Order("film_index.id ASC").
			Limit(snapshotBuildBatchSize).
			Find(&indexes).Error; err != nil {
			return err
		}
		if len(indexes) == 0 {
			break
		}

		snapshots := make([]model.FilmListSnapshot, 0, len(indexes))
		for _, index := range indexes {
			snapshots = append(snapshots, buildFilmListSnapshot(version, index))
			lastID = index.ID
		}
		if err := db.Mdb.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(snapshots, snapshotBuildBatchSize).Error; err != nil {
			return err
		}
		total += len(snapshots)
		log.Printf(
			"[Snapshot] 构建进度 version=%s total=%d batch=%d last_id=%d cost=%s total_cost=%s",
			version,
			total,
			len(snapshots),
			lastID,
			time.Since(batchStartedAt),
			time.Since(startedAt),
		)
	}

	return nil
}

func ActivateRebuiltFilmListSnapshot(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		version = NewSnapshotVersion()
	}
	if err := RebuildFilmListSnapshot(version); err != nil {
		return err
	}
	if err := LoadActiveFilmReadModel(version); err != nil {
		return err
	}
	if err := SetActiveSnapshotVersion(version); err != nil {
		return err
	}
	RefreshAccessDataCaches()
	ClearAdminFilmSearchCache()
	pruneOldFilmListSnapshots(snapshotRetainVersions)
	return nil
}

func EnsureActiveFilmListSnapshot() error {
	var dbCount int64
	if err := db.Mdb.Model(&model.FilmIndex{}).
		Joins("JOIN " + model.TableMovieDetail + " ON " + model.TableMovieDetail + ".mid = film_index.mid AND " + model.TableMovieDetail + ".deleted_at IS NULL").
		Count(&dbCount).Error; err != nil {
		return err
	}
	if dbCount == 0 {
		return nil
	}

	activeVer := strings.TrimSpace(GetActiveSnapshotVersion())
	var snapCount int64
	if activeVer != "" {
		_ = db.Mdb.Model(&model.FilmListSnapshot{}).
			Where("snapshot_version = ?", activeVer).
			Count(&snapCount).Error
	}

	// 退出时清空了版本号（activeVer == ""），或异常强杀导致快照记录数与库内影片数不一致
	if activeVer == "" || snapCount != dbCount {
		refreshMissingPlayFromSummaries()
		version := NewSnapshotVersion()
		if err := ActivateRebuiltFilmListSnapshot(version); err != nil {
			return err
		}
		log.Printf("[Snapshot] 已基于现有影片数据构建并激活前台快照, version=%s, film_count=%d (原快照=%d)", version, dbCount, snapCount)
		return nil
	}

	return nil
}

func refreshMissingPlayFromSummaries() {
	_, _ = FlushPendingPlaySummaryRefresh()

	var missingMIDs []int64
	if err := db.Mdb.Model(&model.FilmIndex{}).
		Joins("JOIN "+model.TableMovieDetail+" ON "+model.TableMovieDetail+".mid = film_index.mid AND "+model.TableMovieDetail+".deleted_at IS NULL").
		Where("film_index.play_from_summary = ? OR film_index.play_from_summary IS NULL", "").
		Pluck("film_index.mid", &missingMIDs).Error; err != nil {
		log.Printf("[Snapshot] 查询缺失播放源影片失败: %v", err)
		return
	}
	if len(missingMIDs) == 0 {
		return
	}
	midSet := make(map[int64]struct{}, len(missingMIDs))
	for _, mid := range missingMIDs {
		if mid > 0 {
			midSet[mid] = struct{}{}
		}
	}
	if err := flushPlaySummaryRefreshMids(midSet); err != nil {
		log.Printf("[Snapshot] 刷新缺失播放源摘要失败: %v", err)
	}
}

func pruneOldFilmListSnapshots(retain int) {
	if retain <= 0 {
		retain = 1
	}

	var versions []string
	if err := db.Mdb.Model(&model.FilmListSnapshot{}).
		Select("snapshot_version").
		Group("snapshot_version").
		Order("MAX(id) DESC").
		Limit(retain).
		Pluck("snapshot_version", &versions).Error; err != nil {
		log.Printf("pruneOldFilmListSnapshots Versions Error: %v", err)
		return
	}
	if len(versions) == 0 {
		return
	}

	// 20w+ 数据量下分批删除旧快照数据，避免单次 DELETE 锁住全表与撑爆 Undo Log
	const pruneChunkSize = 5000
	for {
		res := db.Mdb.Where("snapshot_version NOT IN ?", versions).Limit(pruneChunkSize).Unscoped().Delete(&model.FilmListSnapshot{})
		if res.Error != nil {
			log.Printf("pruneOldFilmListSnapshots Delete Error: %v", res.Error)
			break
		}
		if res.RowsAffected == 0 {
			break
		}
	}
}
