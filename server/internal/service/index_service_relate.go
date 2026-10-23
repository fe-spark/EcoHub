package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	filmshared "server/internal/repository/film/shared"
	filmsnapshot "server/internal/repository/film/snapshot"

	"golang.org/x/sync/singleflight"
)

var relateMovieSfGroup singleflight.Group

// RelateMovie 根据当前影片快照匹配相关的影片（默认锁定首选站）
func (i *IndexService) RelateMovie(mid int64, page *dto.Page, sourceIdOpt ...string) ([]model.MovieBasicInfo, error) {
	if mid <= 0 {
		return []model.MovieBasicInfo{}, nil
	}
	startedAt := time.Now()
	page = normalizeIndexPage(page)
	version := filmsnapshot.GetActiveReadModelVersion()
	if version == "" {
		version = filmsnapshot.GetActiveSnapshotVersion()
	}
	if version == "" {
		return []model.MovieBasicInfo{}, nil
	}

	var sourceID string
	if len(sourceIdOpt) > 0 {
		sourceID = strings.TrimSpace(sourceIdOpt[0])
	}
	if sourceID == "" {
		sourceID = i.BaselineSourceID()
	}

	var cacheKey string
	if sourceID != "" {
		cacheKey = fmt.Sprintf("%s:v%s:src_%s:%d:p%d:s%d", config.FilmRelateVOCachePrefix, version, sourceID, mid, page.Current, page.PageSize)
	} else {
		cacheKey = fmt.Sprintf("%s:v%s:%d:p%d:s%d", config.FilmRelateVOCachePrefix, version, mid, page.Current, page.PageSize)
	}
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			if data == "[]" {
				return []model.MovieBasicInfo{}, nil
			}
			var cached []model.MovieBasicInfo
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached, nil
			}
		}
	}

	val, err, _ := relateMovieSfGroup.Do(cacheKey, func() (any, error) {
		if db.Rdb != nil {
			if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
				if data == "[]" {
					return []model.MovieBasicInfo{}, nil
				}
				var cached []model.MovieBasicInfo
				if json.Unmarshal([]byte(data), &cached) == nil {
					return cached, nil
				}
			}
		}

		snapshotStartedAt := time.Now()
		snapshot, snapErr := filmsnapshot.GetSnapshotByMid(version, mid)
		logSlowIndexServiceStep("RelateMovie.snapshot", snapshotStartedAt, "id", mid)
		if snapErr != nil {
			return nil, snapErr
		}
		if snapshot == nil {
			if db.Rdb != nil {
				_ = db.Rdb.Set(db.Cxt, cacheKey, "[]", 60*time.Second).Err()
			}
			return []model.MovieBasicInfo{}, nil
		}
		exists, detailErr := filmsnapshot.HasMovieDetail(snapshot.Mid)
		if detailErr != nil {
			return nil, detailErr
		}
		if !exists {
			filmsnapshot.DeleteActiveSnapshotsByMids(snapshot.Mid)
			if db.Rdb != nil {
				_ = db.Rdb.Set(db.Cxt, cacheKey, "[]", 60*time.Second).Err()
			}
			return []model.MovieBasicInfo{}, nil
		}
		listStartedAt := time.Now()
		list, listErr := filmsnapshot.ListRelatedSnapshotsReadModel(version, *snapshot, page, sourceID)
		if listErr != nil {
			return nil, listErr
		}
		logSlowIndexServiceStep("RelateMovie.list", listStartedAt, "id", mid)
		buildStartedAt := time.Now()
		result := filmshared.BuildMovieBasicInfosFromSnapshots(list...)
		logSlowIndexServiceStep("RelateMovie.build", buildStartedAt, "id", mid)
		logSlowIndexServiceStep("RelateMovie.total", startedAt, "id", mid)

		if result == nil {
			result = []model.MovieBasicInfo{}
		}

		if db.Rdb != nil {
			if len(result) == 0 {
				_ = db.Rdb.Set(db.Cxt, cacheKey, "[]", 60*time.Second).Err()
			} else {
				if raw, err := json.Marshal(result); err == nil {
					_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), time.Hour).Err()
				}
			}
		}
		return result, nil
	})

	if err != nil || val == nil {
		if err != nil {
			return nil, err
		}
		return []model.MovieBasicInfo{}, nil
	}
	result, ok := val.([]model.MovieBasicInfo)
	if !ok {
		return nil, errors.New("相关推荐结果异常")
	}
	res := make([]model.MovieBasicInfo, len(result))
	copy(res, result)
	return res, nil
}
