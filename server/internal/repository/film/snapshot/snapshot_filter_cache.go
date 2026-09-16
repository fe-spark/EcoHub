package snapshot

import (
	"server/internal/model"
	"server/internal/model/dto"
)

func SearchSnapshotsByKeywordAndSortFast(version string, keyword string, sortField string, page *dto.Page) []model.FilmListSnapshot {
	return SearchSnapshotsByKeywordAndSortReadModel(version, keyword, sortField, page)
}

func ListProvideSnapshotsFast(version string, st model.SearchTagsVO, keyword string, recentHours int, page *dto.Page) []model.FilmListSnapshot {
	return ListProvideSnapshotsReadModel(version, st, keyword, recentHours, page)
}

func ListFilmSnapshotsByTagsFast(version string, st model.SearchTagsVO, page *dto.Page) []model.FilmListSnapshot {
	return ListFilmSnapshotsByTagsReadModel(version, st, page)
}
