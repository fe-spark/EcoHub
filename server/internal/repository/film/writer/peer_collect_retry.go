package writer

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"server/internal/repository"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

type peerPageTiming struct {
	path     string
	mapCost  time.Duration
	playlist time.Duration
	remarks  time.Duration
}

type peerTimingKey struct{}

func peerTimingContext(ctx context.Context) (context.Context, *peerPageTiming) {
	if ctx == nil {
		ctx = context.Background()
	}
	timing := &peerPageTiming{}
	return context.WithValue(ctx, peerTimingKey{}, timing), timing
}

func notePeerTiming(tx *gorm.DB, path string, mapCost, playlist, remarks time.Duration) {
	if tx == nil || tx.Statement == nil || tx.Statement.Context == nil {
		return
	}
	timing, _ := tx.Statement.Context.Value(peerTimingKey{}).(*peerPageTiming)
	if timing == nil {
		return
	}
	timing.path = path
	timing.mapCost = mapCost
	timing.playlist = playlist
	timing.remarks = remarks
}

func isRetryableMySQLLockErr(err error) bool {
	if err == nil {
		return false
	}
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr != nil && (mysqlErr.Number == 1213 || mysqlErr.Number == 1205) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "deadlock found") || strings.Contains(msg, "lock wait timeout")
}

func runPeerCollectTxWithRetry(ctx context.Context, fn func(tx *gorm.DB) error) error {
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		err = runPeerCollectTx(ctx, fn)
		if err == nil || !isRetryableMySQLLockErr(err) || attempt == 3 {
			return err
		}
		time.Sleep(time.Duration(attempt) * 300 * time.Millisecond)
	}
	return err
}

func isDeterministicRowError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Data too long") || strings.Contains(msg, "Error 1406")
}

func prepareSourceOtherCategory(sourceID string) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || sourceID == "manual" {
		return
	}
	if _, _, err := repository.EnsureOtherCategory(sourceID); err != nil {
		log.Printf("[FilmWrite] 补建「其他」分类失败 source=%s err=%v", sourceID, err)
	}
}
