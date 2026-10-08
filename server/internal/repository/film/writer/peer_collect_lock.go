package writer

import (
	"errors"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/film/shared"
	"server/internal/repository/support"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var filmMidLocks [2048]sync.Mutex

func getFilmMidLock(mid int64) *sync.Mutex {
	if mid <= 0 {
		return &filmMidLocks[0]
	}
	return &filmMidLocks[uint64(mid)%2048]
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
	pid := detail.RawPid
	if local := support.GetRootId(support.GetLocalCategoryId(source.Id, detail.Cid)); local > 0 {
		pid = local
	} else if local := support.GetRootId(support.GetLocalCategoryId(source.Id, detail.RawPid)); local > 0 {
		pid = local
	}
	allKeys := shared.BuildMovieMatchKeysWithCategory(detail.DbId, detail.Name, pid)
	if len(allKeys) == 0 {
		return 0
	}
	var matchKey model.MovieMatchKey
	if err := db.Mdb.Where("match_key IN ?", allKeys).Order("id ASC").First(&matchKey).Error; err == nil {
		return matchKey.Mid
	}
	return 0
}

func lockMatchKeysTx(tx *gorm.DB, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	var rows []model.MovieMatchKey
	q := tx.Where("match_key IN ?", sorted).Order("match_key ASC")
	if tx.Dialector != nil && tx.Dialector.Name() == "mysql" {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return q.Find(&rows).Error
}

func isRetryableDBErr(err error) bool {
	if err == nil {
		return false
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1213 || mysqlErr.Number == 1205 || mysqlErr.Number == 1062
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "deadlock") ||
		strings.Contains(msg, "lock wait timeout") ||
		strings.Contains(msg, "duplicate")
}

func saveSinglePeerDetailWithRetry(source *model.FilmSource, detail model.MovieDetail) (mid int64, isNew bool, playLinesChanged bool, err error) {
	const maxAttempt = 8
	for attempt := 1; attempt <= maxAttempt; attempt++ {
		mid, isNew, playLinesChanged, err = saveSinglePeerDetailTx(source, detail)
		if err == nil || !isRetryableDBErr(err) || attempt == maxAttempt {
			break
		}
		time.Sleep(time.Duration(20+rand.Intn(40)*attempt) * time.Millisecond)
	}
	return mid, isNew, playLinesChanged, err
}

func lockPeerCollect(source *model.FilmSource, detail model.MovieDetail) (unlock func()) {
	identLock := getFilmIdentityLock(detail.Name)
	identLock.Lock()
	sourceLock := getSourceWriteLock(source.Id)
	sourceLock.Lock()
	peekMid := peekPeerFilmMid(source, detail)
	var midLock *sync.Mutex
	if peekMid > 0 {
		midLock = getFilmMidLock(peekMid)
		midLock.Lock()
	}
	return func() {
		if midLock != nil {
			midLock.Unlock()
		}
		sourceLock.Unlock()
		identLock.Unlock()
	}
}
