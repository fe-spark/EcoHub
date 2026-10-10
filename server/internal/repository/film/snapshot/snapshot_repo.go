package snapshot

import (
	"log"
	"strings"
	"sync"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
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

func ClearActiveSnapshotVersion() {
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
	ClearActiveSnapshotVersion()
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

func ActivateRebuiltFilmListSnapshot(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		version = EnsureLiveReadVersion()
	} else if err := SetActiveSnapshotVersion(version); err != nil {
		return err
	}
	if err := LoadActiveFilmReadModel(version); err != nil {
		return err
	}
	RefreshAccessDataCaches()
	return nil
}

func EnsureActiveFilmListSnapshot() error {
	var dbCount int64
	if err := db.Mdb.Model(&model.FilmIndex{}).Count(&dbCount).Error; err != nil {
		return err
	}
	if dbCount == 0 {
		return nil
	}

	version := EnsureLiveReadVersion()
	if err := LoadActiveFilmReadModel(version); err != nil {
		return err
	}
	log.Printf("[Snapshot] 列表直读 film_index 已就绪 version=%s film_count=%d", version, dbCount)
	return nil
}

func RefreshMissingPlayFromSummaries() {
	_, _ = FlushPendingPlaySummaryRefresh()

	var missingMIDs []int64
	if err := db.Mdb.Model(&model.FilmIndex{}).
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

func HasPublishedFilmListSnapshot() (bool, error) {
	if db.Mdb == nil {
		return false, nil
	}
	var count int64
	if err := db.Mdb.Model(&model.FilmIndex{}).Limit(1).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
