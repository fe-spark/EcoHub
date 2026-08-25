package film

import (
	"log"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository/support"
)

func GetSnapshotMovieListByCategoryReadModel(version string, field string, id int64, limit int, offset int) []model.MovieBasicInfo {
	startedAt := time.Now()
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	id = support.ResolveCategoryID(id)
	if version == "" || id <= 0 || limit <= 0 {
		return []model.MovieBasicInfo{}
	}
	if offset < 0 {
		offset = 0
	}

	query := db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", version)
	if field == "pid" {
		query = query.Where("pid = ?", id)
	} else {
		query = query.Where("cid = ?", id)
	}

	var snapshots []model.FilmListSnapshot
	if err := query.Order("update_stamp DESC, id DESC").Offset(offset).Limit(limit).Find(&snapshots).Error; err != nil {
		return []model.MovieBasicInfo{}
	}
	log.Printf("[FilmCategoryList] 获取分类列表 field=%s id=%d count=%d offset=%d limit=%d cost=%s",
		field, id, len(snapshots), offset, limit, time.Since(startedAt))
	return BuildMovieBasicInfosFromSnapshots(snapshots...)
}

func GetSnapshotMovieListByCategoryPageReadModel(version string, field string, id int64, page *dto.Page) []model.MovieBasicInfo {
	startedAt := time.Now()
	page = ensurePage(page)
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	id = support.ResolveCategoryID(id)
	if version == "" || id <= 0 {
		return []model.MovieBasicInfo{}
	}

	query := db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", version)
	if field == "pid" {
		query = query.Where("pid = ?", id)
	} else {
		query = query.Where("cid = ?", id)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return []model.MovieBasicInfo{}
	}
	page.Total = int(total)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}

	var snapshots []model.FilmListSnapshot
	offset := getPageOffset(page)
	if err := query.Order("update_stamp DESC, id DESC").Offset(offset).Limit(page.PageSize).Find(&snapshots).Error; err != nil {
		return []model.MovieBasicInfo{}
	}
	log.Printf("[FilmCategoryList] 获取分类分页列表 field=%s id=%d total=%d page=%d size=%d cost=%s",
		field, id, page.Total, page.Current, len(snapshots), time.Since(startedAt))
	return BuildMovieBasicInfosFromSnapshots(snapshots...)
}

func GetSnapshotHotMovieListByCategoryReadModel(version string, field string, id int64, limit int, offset int) []model.MovieBasicInfo {
	startedAt := time.Now()
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	id = support.ResolveCategoryID(id)
	if version == "" || id <= 0 || limit <= 0 {
		return []model.MovieBasicInfo{}
	}
	if offset < 0 {
		offset = 0
	}

	hotSince := time.Now().AddDate(0, -1, 0).Unix()
	query := db.Mdb.Model(&model.FilmListSnapshot{}).
		Where("snapshot_version = ? AND update_stamp > ?", version, hotSince)
	if field == "pid" {
		query = query.Where("pid = ?", id)
	} else {
		query = query.Where("cid = ?", id)
	}

	var snapshots []model.FilmListSnapshot
	if err := query.Order("year DESC, hits DESC, id DESC").Offset(offset).Limit(limit).Find(&snapshots).Error; err != nil {
		return []model.MovieBasicInfo{}
	}
	log.Printf("[FilmHotList] 获取分类热播列表 field=%s id=%d count=%d offset=%d limit=%d cost=%s",
		field, id, len(snapshots), offset, limit, time.Since(startedAt))
	return BuildMovieBasicInfosFromSnapshots(snapshots...)
}

func GetSnapshotMovieListBySortReadModel(version string, sortType int, pid int64, page *dto.Page) []model.MovieBasicInfo {
	startedAt := time.Now()
	page = ensurePage(page)
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	pid = support.ResolveCategoryID(pid)
	if version == "" || pid <= 0 {
		return []model.MovieBasicInfo{}
	}

	query := db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ? AND pid = ?", version, pid)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return []model.MovieBasicInfo{}
	}
	page.Total = int(total)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}

	orderClause := "update_stamp DESC, id DESC"
	switch sortType {
	case 0:
		orderClause = "year DESC, update_stamp DESC, id DESC"
	case 1:
		orderClause = "hits DESC, id DESC"
	case 2:
		orderClause = "update_stamp DESC, id DESC"
	}

	var snapshots []model.FilmListSnapshot
	offset := getPageOffset(page)
	if err := query.Order(orderClause).Offset(offset).Limit(page.PageSize).Find(&snapshots).Error; err != nil {
		return []model.MovieBasicInfo{}
	}
	log.Printf("[FilmSortList] 获取分类排序列表 pid=%d sortType=%d total=%d page=%d size=%d cost=%s",
		pid, sortType, page.Total, page.Current, len(snapshots), time.Since(startedAt))
	return BuildMovieBasicInfosFromSnapshots(snapshots...)
}


