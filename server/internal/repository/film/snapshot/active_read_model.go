package snapshot

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
)

type FilmReadModel struct {
	Version string
}

var activeFilmReadModel atomic.Pointer[FilmReadModel]
var activeFilmReadModelMu sync.Mutex

func init() {
	activeFilmReadModel.Store(&FilmReadModel{Version: ""})
}

func LoadActiveFilmReadModel(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	activeFilmReadModelMu.Lock()
	defer activeFilmReadModelMu.Unlock()
	activeFilmReadModel.Store(&FilmReadModel{Version: version})
	activeFilmSearchMetasMu.Lock()
	activeFilmSearchMetas.Store(nil)
	activeFilmSearchMetasMu.Unlock()
	if version != "" {
		searchMetaBuildWg.Add(1)
		go func(ver string) {
			defer searchMetaBuildWg.Done()
			_ = loadFilmSearchMetaIndex(ver)
		}(version)
	}
	log.Printf("[ActiveReadModel] 活跃读模型已就绪 version=%s", version)
	return nil
}

func RefreshActiveProjectedReadModel() error {
	RefreshAccessDataCaches()
	return nil
}

func ApplyActiveFilmReadModelSnapshots(version string, snapshots []model.FilmListSnapshot, deletedMIDs []int64) error {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	activeFilmSearchMetasMu.Lock()
	activeFilmSearchMetas.Store(nil)
	activeFilmSearchMetasMu.Unlock()
	if version != "" {
		searchMetaBuildWg.Add(1)
		go func(ver string) {
			defer searchMetaBuildWg.Done()
			_ = loadFilmSearchMetaIndex(ver)
		}(version)
	}
	RefreshAccessDataCaches()
	return nil
}

func ClearActiveFilmReadModel() {
	activeFilmReadModel.Store(&FilmReadModel{Version: ""})
	activeFilmSearchMetasMu.Lock()
	activeFilmSearchMetas.Store(nil)
	activeFilmSearchMetasMu.Unlock()
}

type searchCacheVerState struct {
	version   string
	updatedAt time.Time
}

var searchCacheVer atomic.Pointer[searchCacheVerState]
var searchVerSeq uint64

const searchVerCacheTTL = 1 * time.Second

func BumpSearchCacheVersion() {
	seq := atomic.AddUint64(&searchVerSeq, 1)
	newVer := fmt.Sprintf("%d_%d", time.Now().UnixNano(), seq)
	if db.Rdb != nil {
		db.Rdb.Set(db.Cxt, config.SearchCacheVersionKey, newVer, 0)
	}
	searchCacheVer.Store(&searchCacheVerState{
		version:   newVer,
		updatedAt: time.Now(),
	})
}

func GetSearchCacheVersion() string {
	cur := searchCacheVer.Load()
	if cur != nil && cur.version != "" && time.Since(cur.updatedAt) < searchVerCacheTTL {
		return cur.version
	}

	if db.Rdb == nil {
		if cur != nil && cur.version != "" {
			return cur.version
		}
		newVer := fmt.Sprintf("%d", time.Now().UnixNano())
		searchCacheVer.Store(&searchCacheVerState{
			version:   newVer,
			updatedAt: time.Now(),
		})
		return newVer
	}

	version, err := db.Rdb.Get(db.Cxt, config.SearchCacheVersionKey).Result()
	if err == nil && version != "" {
		searchCacheVer.Store(&searchCacheVerState{
			version:   version,
			updatedAt: time.Now(),
		})
		return version
	}

	version = fmt.Sprintf("%d", time.Now().UnixNano())
	if set, _ := db.Rdb.SetNX(db.Cxt, config.SearchCacheVersionKey, version, 0).Result(); !set {
		if curVer, err := db.Rdb.Get(db.Cxt, config.SearchCacheVersionKey).Result(); err == nil && curVer != "" {
			version = curVer
		}
	}
	searchCacheVer.Store(&searchCacheVerState{
		version:   version,
		updatedAt: time.Now(),
	})
	return version
}

// ResetSearchCacheVersionForTest 重置内存缓存版本（仅供测试隔离使用）
func ResetSearchCacheVersionForTest() {
	searchCacheVer.Store(nil)
}

func GetActiveFilmReadModel() *FilmReadModel {
	return activeFilmReadModel.Load()
}

func GetProjectedSnapshotsByMidsOrdered(version string, mids []int64) []model.FilmListSnapshot {
	return GetSnapshotsByMidsOrdered(version, mids)
}
