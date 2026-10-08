package writer

import (
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/film/shared"
	"server/internal/repository/support"
	"server/internal/utils"

	"gorm.io/gorm"
)

const sqlFilmLockWaitSec = 60

var (
	filmMidLocks      [2048]sync.Mutex
	filmMatchKeyLocks [2048]sync.Mutex
)

func getFilmMidLock(mid int64) *sync.Mutex {
	if mid <= 0 {
		return &filmMidLocks[0]
	}
	return &filmMidLocks[uint64(mid)%2048]
}

func getMatchKeyLock(key string) *sync.Mutex {
	key = strings.TrimSpace(key)
	if key == "" {
		return &filmMatchKeyLocks[0]
	}
	hash := utils.GenerateHashKey(key)
	var idx uint64
	if len(hash) >= 8 {
		idx, _ = strconv.ParseUint(hash[:8], 16, 32)
	}
	return &filmMatchKeyLocks[idx%2048]
}

func peerMatchKeys(source *model.FilmSource, detail model.MovieDetail) []string {
	pid := detail.RawPid
	if source != nil {
		if local := support.GetRootId(support.GetLocalCategoryId(source.Id, detail.Cid)); local > 0 {
			pid = local
		} else if local := support.GetRootId(support.GetLocalCategoryId(source.Id, detail.RawPid)); local > 0 {
			pid = local
		}
	}
	keys := shared.BuildMovieMatchKeysWithCategory(detail.DbId, detail.Name, pid)
	sort.Strings(keys)
	return keys
}

func peekPeerFilmMid(source *model.FilmSource, detail model.MovieDetail) int64 {
	if db.Mdb == nil || source == nil {
		return 0
	}
	var mapping model.MovieSourceMapping
	if err := db.Mdb.Where("source_id = ? AND source_mid = ?", source.Id, detail.Id).
		First(&mapping).Error; err == nil && mapping.GlobalMid > 0 {
		return mapping.GlobalMid
	}
	allKeys := peerMatchKeys(source, detail)
	if len(allKeys) == 0 {
		return 0
	}
	var matchKey model.MovieMatchKey
	if err := db.Mdb.Where("match_key IN ?", allKeys).Order("id ASC").First(&matchKey).Error; err == nil {
		return matchKey.Mid
	}
	return 0
}

func sqlFilmLockNames(source *model.FilmSource, detail model.MovieDetail, mid int64) []string {
	keys := peerMatchKeys(source, detail)
	names := make([]string, 0, len(keys)+1)
	for _, key := range keys {
		names = append(names, "eh:k:"+utils.GenerateHashKey(key))
	}
	if mid > 0 {
		names = append(names, fmt.Sprintf("eh:m:%d", mid))
	}
	sort.Strings(names)
	return names
}

func acquireSQLFilmLocks(tx *gorm.DB, names []string) error {
	if tx == nil || tx.Dialector == nil || tx.Dialector.Name() != "mysql" || len(names) == 0 {
		return nil
	}
	for _, name := range names {
		var got sql.NullInt64
		if err := tx.Raw("SELECT GET_LOCK(?, ?)", name, sqlFilmLockWaitSec).Scan(&got).Error; err != nil {
			_ = releaseSQLFilmLocks(tx, names)
			return err
		}
		if !got.Valid || got.Int64 != 1 {
			_ = releaseSQLFilmLocks(tx, names)
			return fmt.Errorf("等待影片写锁超时")
		}
	}
	return nil
}

func releaseSQLFilmLocks(tx *gorm.DB, names []string) error {
	if tx == nil || tx.Dialector == nil || tx.Dialector.Name() != "mysql" {
		return nil
	}
	for i := len(names) - 1; i >= 0; i-- {
		_ = tx.Exec("SELECT RELEASE_LOCK(?)", names[i]).Error
	}
	return nil
}

func lockPeerCollect(source *model.FilmSource, detail model.MovieDetail) func() {
	held := make([]*sync.Mutex, 0, 8)
	seen := make(map[*sync.Mutex]struct{}, 8)
	lockOne := func(l *sync.Mutex) {
		if l == nil {
			return
		}
		if _, ok := seen[l]; ok {
			return
		}
		seen[l] = struct{}{}
		l.Lock()
		held = append(held, l)
	}
	for _, key := range peerMatchKeys(source, detail) {
		lockOne(getMatchKeyLock(key))
	}
	if source != nil {
		lockOne(getSourceWriteLock(source.Id))
	}
	if mid := peekPeerFilmMid(source, detail); mid > 0 {
		lockOne(getFilmMidLock(mid))
	}
	return func() {
		for i := len(held) - 1; i >= 0; i-- {
			held[i].Unlock()
		}
	}
}

func saveSinglePeerDetailSynced(source *model.FilmSource, detail model.MovieDetail) (int64, bool, bool, error) {
	unlock := lockPeerCollect(source, detail)
	defer unlock()
	return saveSinglePeerDetailTx(source, detail)
}
