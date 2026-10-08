package service

import (
	"encoding/json"
	"fmt"
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

// RelateMovie 根据当前影片快照匹配相关的影片
func (i *IndexService) RelateMovie(mid int64, page *dto.Page) []model.MovieBasicInfo {
	if mid <= 0 {
		return []model.MovieBasicInfo{}
	}
	startedAt := time.Now()
	page = normalizeIndexPage(page)
	version := filmsnapshot.GetActiveReadModelVersion()
	if version == "" {
		version = filmsnapshot.GetActiveSnapshotVersion()
	}
	if version == "" {
		return []model.MovieBasicInfo{}
	}

	cacheKey := fmt.Sprintf("%s:v%s:%d:p%d:s%d", config.FilmRelateVOCachePrefix, version, mid, page.Current, page.PageSize)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			if data == "[]" {
				return []model.MovieBasicInfo{}
			}
			var cached []model.MovieBasicInfo
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached
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
		snapshot := filmsnapshot.GetSnapshotByMid(version, mid)
		logSlowIndexServiceStep("RelateMovie.snapshot", snapshotStartedAt, "id", mid)
		if snapshot == nil {
			if db.Rdb != nil {
				_ = db.Rdb.Set(db.Cxt, cacheKey, "[]", 60*time.Second).Err()
			}
			return []model.MovieBasicInfo{}, nil
		}
		if !filmsnapshot.HasMovieDetail(snapshot.Mid) {
			filmsnapshot.DeleteActiveSnapshotsByMids(snapshot.Mid)
			if db.Rdb != nil {
				_ = db.Rdb.Set(db.Cxt, cacheKey, "[]", 60*time.Second).Err()
			}
			return []model.MovieBasicInfo{}, nil
		}
		listStartedAt := time.Now()
		list := filmsnapshot.ListRelatedSnapshotsReadModel(version, *snapshot, page)
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
		return []model.MovieBasicInfo{}
	}
	result, ok := val.([]model.MovieBasicInfo)
	if !ok {
		return []model.MovieBasicInfo{}
	}
	res := make([]model.MovieBasicInfo, len(result))
	copy(res, result)
	return res
}
