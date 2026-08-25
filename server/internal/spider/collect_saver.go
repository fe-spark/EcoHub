package spider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"

	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
)

var sourceWriteLocks sync.Map

func getSourceWriteLock(sourceID string) *sync.Mutex {
	if lock, ok := sourceWriteLocks.Load(sourceID); ok {
		return lock.(*sync.Mutex)
	}
	lock := &sync.Mutex{}
	actual, _ := sourceWriteLocks.LoadOrStore(sourceID, lock)
	return actual.(*sync.Mutex)
}

func runCollectDBWriteWithRetry(ctx context.Context, sourceName string, page int, write func() error) error {
	var err error
	for attempt := 1; attempt <= collectDBWriteRetries; attempt++ {
		err = write()
		if err == nil || !isRetryableDBWriteErr(err) || attempt == collectDBWriteRetries {
			return err
		}
		backoff := time.Duration(attempt*300) * time.Millisecond
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
	return err
}

func isRetryableDBWriteErr(err error) bool {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1213 || mysqlErr.Number == 1205
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "deadlock found") || strings.Contains(message, "lock wait timeout")
}

func saveSlavePlaylists(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) ([]int64, error) {
	lock := getSourceWriteLock(s.Id)
	lock.Lock()
	defer lock.Unlock()
	var changedMids []int64
	err := runCollectDBWriteWithRetry(ctx, s.Name, page, func() error {
		mids, err := filmrepo.SaveSitePlayList(s.Id, list)
		if err != nil {
			return err
		}
		changedMids = mids
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("save slave playlists failed: %w", err)
	}
	return changedMids, nil
}

func saveCollectedFilm(s *model.FilmSource, list []model.MovieDetail, saveMaster func(string, []model.MovieDetail) error) error {
	switch s.Grade {
	case model.MasterCollect:
		lock := getSourceWriteLock(s.Id)
		lock.Lock()
		defer lock.Unlock()
		if err := saveMaster(s.Id, list); err != nil {
			return fmt.Errorf("save master details failed: %w", err)
		}
		return nil
	case model.SlaveCollect:
		_, err := saveSlavePlaylists(context.Background(), s, 0, list)
		return err
	}
	return nil
}

type collectWriteMids struct {
	Notify   []int64
	Affected []int64
}

func saveCollectedFilmForCollect(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) (collectWriteMids, error) {
	if s.Grade != model.MasterCollect {
		mids, err := saveSlavePlaylists(ctx, s, page, list)
		if err != nil {
			return collectWriteMids{}, err
		}
		return collectWriteMids{Notify: mids, Affected: mids}, nil
	}

	var result filmrepo.CollectWriteResult
	lock := getSourceWriteLock(s.Id)
	lock.Lock()
	defer lock.Unlock()
	err := runCollectDBWriteWithRetry(ctx, s.Name, page, func() error {
		r, err := filmrepo.SaveDetailsForCollect(s.Id, list)
		if err != nil {
			return err
		}
		result = r
		return nil
	})
	if err != nil {
		return collectWriteMids{}, fmt.Errorf("save master details failed: %w", err)
	}
	return collectWriteMids{Notify: result.NotifyMIDs, Affected: result.AffectedMIDs}, nil
}

func saveFilmPageFailure(s *model.FilmSource, h, pg int, phase string, err error) {
	if err == nil {
		err = errors.New("unknown error")
	}
	recordErr := repository.SaveFailureRecord(model.FailureRecord{
		OriginId:   s.Id,
		OriginName: s.Name,
		Uri:        s.Uri,
		PageNumber: pg,
		Hour:       h,
		Cause:      fmt.Sprintf("%s: %v", phase, err),
		Status:     model.FailureRecordStatusPending,
	})
	if recordErr != nil {
		syslog.Errorf("[Spider][Failure] 失败页记录保存失败 source_id=%s source=%s page=%d hour=%d phase=%s err=%v record_err=%v", s.Id, s.Name, pg, h, phase, err, recordErr)
	}
}
