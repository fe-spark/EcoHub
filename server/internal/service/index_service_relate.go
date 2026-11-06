package service

import (
	"strings"
	"time"

	"server/internal/model"
	"server/internal/model/dto"
	filmshared "server/internal/repository/film/shared"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/repository/support"
)

// RelateMovie 根据当前影片快照匹配相关的影片（默认锁定首选站）。
// 候选片单由检索层缓存。这里不再另存一份结果，也不写空列表。
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

	snapshotStartedAt := time.Now()
	snapshot, snapErr := filmsnapshot.GetSnapshotByMid(version, mid)
	logSlowIndexServiceStep("RelateMovie.snapshot", snapshotStartedAt, "id", mid)
	if snapErr != nil {
		return nil, snapErr
	}
	if snapshot == nil || !support.IsCategoryVisible(snapshot.Cid, snapshot.Pid) {
		return []model.MovieBasicInfo{}, nil
	}
	exists, detailErr := filmsnapshot.HasMovieDetail(snapshot.Mid)
	if detailErr != nil {
		return nil, detailErr
	}
	if !exists {
		filmsnapshot.DeleteActiveSnapshotsByMids(snapshot.Mid)
		return []model.MovieBasicInfo{}, nil
	}

	listStartedAt := time.Now()
	list, listErr := filmsnapshot.ListRelatedSnapshotsReadModel(version, *snapshot, page, sourceID)
	if listErr != nil {
		return nil, listErr
	}
	visibleList := make([]model.FilmListSnapshot, 0, len(list))
	for _, item := range list {
		if support.IsCategoryVisible(item.Cid, item.Pid) {
			visibleList = append(visibleList, item)
		}
	}
	logSlowIndexServiceStep("RelateMovie.list", listStartedAt, "id", mid)
	buildStartedAt := time.Now()
	result := filmshared.BuildMovieBasicInfosFromSnapshots(visibleList...)
	logSlowIndexServiceStep("RelateMovie.build", buildStartedAt, "id", mid)
	logSlowIndexServiceStep("RelateMovie.total", startedAt, "id", mid)
	if result == nil {
		return []model.MovieBasicInfo{}, nil
	}
	return result, nil
}
