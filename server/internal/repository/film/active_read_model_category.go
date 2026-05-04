package film

import (
	"sort"
	"time"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository/support"
)

func GetSnapshotMovieListByCategoryReadModel(version string, field string, id int64, limit int, offset int) []model.MovieBasicInfo {
	readModel := requireActiveFilmReadModel(version)
	snapshots := readModel.projectedCategorySnapshots(field, id)
	sortSnapshotsBySearchTag(snapshots, "update_stamp")
	return BuildMovieBasicInfosFromSnapshots(sliceSnapshots(snapshots, limit, offset)...)
}

func GetSnapshotMovieListByCategoryPageReadModel(version string, field string, id int64, page *dto.Page) []model.MovieBasicInfo {
	page = ensurePage(page)
	readModel := requireActiveFilmReadModel(version)
	snapshots := readModel.projectedCategorySnapshots(field, id)
	sortSnapshotsBySearchTag(snapshots, "update_stamp")
	page.Total = len(snapshots)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}
	return BuildMovieBasicInfosFromSnapshots(pageSnapshots(snapshots, page)...)
}

func GetSnapshotHotMovieListByCategoryReadModel(version string, field string, id int64, limit int, offset int) []model.MovieBasicInfo {
	readModel := requireActiveFilmReadModel(version)
	hotSince := time.Now().AddDate(0, -1, 0).Unix()
	all := readModel.projectedCategorySnapshots(field, id)
	snapshots := make([]model.FilmListSnapshot, 0, len(all))
	for _, snapshot := range all {
		if snapshot.UpdateStamp > hotSince {
			snapshots = append(snapshots, snapshot)
		}
	}
	sort.SliceStable(snapshots, func(i, j int) bool {
		if snapshots[i].Year != snapshots[j].Year {
			return snapshots[i].Year > snapshots[j].Year
		}
		if snapshots[i].Hits != snapshots[j].Hits {
			return snapshots[i].Hits > snapshots[j].Hits
		}
		return snapshots[i].Mid > snapshots[j].Mid
	})
	return BuildMovieBasicInfosFromSnapshots(sliceSnapshots(snapshots, limit, offset)...)
}

func GetSnapshotMovieListBySortReadModel(version string, sortType int, pid int64, page *dto.Page) []model.MovieBasicInfo {
	page = ensurePage(page)
	readModel := requireActiveFilmReadModel(version)
	snapshots := readModel.projectedCategorySnapshots("pid", pid)
	switch sortType {
	case 0:
		sort.SliceStable(snapshots, func(i, j int) bool {
			if snapshots[i].Year != snapshots[j].Year {
				return snapshots[i].Year > snapshots[j].Year
			}
			if snapshots[i].UpdateStamp != snapshots[j].UpdateStamp {
				return snapshots[i].UpdateStamp > snapshots[j].UpdateStamp
			}
			return snapshots[i].Mid > snapshots[j].Mid
		})
	case 1:
		sortSnapshotsBySearchTag(snapshots, "hits")
	case 2:
		sortSnapshotsBySearchTag(snapshots, "update_stamp")
	default:
		sortSnapshotsBySearchTag(snapshots, "update_stamp")
	}
	page.Total = len(snapshots)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}
	return BuildMovieBasicInfosFromSnapshots(pageSnapshots(snapshots, page)...)
}

func (m *FilmReadModel) projectedCategorySnapshots(field string, id int64) []model.FilmListSnapshot {
	id = support.ResolveCategoryID(id)
	if id <= 0 {
		return []model.FilmListSnapshot{}
	}
	if field == "pid" {
		return m.projectedSnapshotsByPid(id)
	}
	snapshots := make([]model.FilmListSnapshot, 0)
	for _, snapshot := range m.projectedSnapshots() {
		if support.ResolveCategoryID(snapshot.Cid) == id {
			snapshots = append(snapshots, snapshot)
		}
	}
	return snapshots
}

func sliceSnapshots(snapshots []model.FilmListSnapshot, limit int, offset int) []model.FilmListSnapshot {
	if limit <= 0 {
		return []model.FilmListSnapshot{}
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(snapshots) {
		return []model.FilmListSnapshot{}
	}
	end := offset + limit
	if end > len(snapshots) {
		end = len(snapshots)
	}
	return snapshots[offset:end]
}
