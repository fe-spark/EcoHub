package film

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository/support"
)

const (
	snapshotListCacheTTL = 10 * time.Minute
	snapshotPageCacheTTL = 5 * time.Minute
	basicSelectFields = "id, snapshot_version, mid, pid, cid, c_name, name, score, hits, update_stamp, remarks, state, picture, picture_slide, custom_picture, custom_picture_slide, is_custom_picture, year"
)

type categoryPageCacheItem struct {
	Total     int                   `json:"total"`
	PageCount int                   `json:"page_count"`
	Movies    []model.MovieBasicInfo `json:"movies"`
}

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

	cacheKey := fmt.Sprintf("EcoHub:snap_cat:v%s:%s:%d:%d:%d", version, field, id, limit, offset)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached []model.MovieBasicInfo
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached
			}
		}
	}

	query := db.Mdb.Model(&model.FilmListSnapshot{}).
		Select(basicSelectFields).
		Where("snapshot_version = ?", version)
	if field == "pid" {
		query = query.Where("pid = ?", id)
	} else {
		query = query.Where("cid = ?", id)
	}

	var snapshots []model.FilmListSnapshot
	if err := query.Order("update_stamp DESC, id DESC").Offset(offset).Limit(limit).Find(&snapshots).Error; err != nil {
		return []model.MovieBasicInfo{}
	}
	result := BuildMovieBasicInfosFromSnapshots(snapshots...)

	if db.Rdb != nil && len(result) > 0 {
		if raw, err := json.Marshal(result); err == nil {
			_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), snapshotListCacheTTL).Err()
		}
	}

	log.Printf("[FilmCategoryList] 获取分类列表 field=%s id=%d count=%d offset=%d limit=%d cost=%s",
		field, id, len(result), offset, limit, time.Since(startedAt))
	return result
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

	cacheKey := fmt.Sprintf("EcoHub:snap_cat_page:v%s:%s:%d:p%d:s%d", version, field, id, page.Current, page.PageSize)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var item categoryPageCacheItem
			if json.Unmarshal([]byte(data), &item) == nil {
				page.Total = item.Total
				page.PageCount = item.PageCount
				return item.Movies
			}
		}
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
	if err := query.Select(basicSelectFields).Order("update_stamp DESC, id DESC").Offset(offset).Limit(page.PageSize).Find(&snapshots).Error; err != nil {
		return []model.MovieBasicInfo{}
	}
	result := BuildMovieBasicInfosFromSnapshots(snapshots...)

	if db.Rdb != nil && len(result) > 0 {
		item := categoryPageCacheItem{
			Total:     page.Total,
			PageCount: page.PageCount,
			Movies:    result,
		}
		if raw, err := json.Marshal(item); err == nil {
			_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), snapshotPageCacheTTL).Err()
		}
	}

	log.Printf("[FilmCategoryList] 获取分类分页列表 field=%s id=%d total=%d page=%d size=%d cost=%s",
		field, id, page.Total, page.Current, len(result), time.Since(startedAt))
	return result
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

	cacheKey := fmt.Sprintf("EcoHub:snap_hot:v%s:%s:%d:%d:%d", version, field, id, limit, offset)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached []model.MovieBasicInfo
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached
			}
		}
	}

	query := db.Mdb.Model(&model.FilmListSnapshot{}).
		Select(basicSelectFields).
		Where("snapshot_version = ?", version)
	if field == "pid" {
		query = query.Where("pid = ?", id)
	} else {
		query = query.Where("cid = ?", id)
	}

	var snapshots []model.FilmListSnapshot
	if err := query.Order("hits DESC, id DESC").Offset(offset).Limit(limit).Find(&snapshots).Error; err != nil {
		return []model.MovieBasicInfo{}
	}
	result := BuildMovieBasicInfosFromSnapshots(snapshots...)

	if db.Rdb != nil && len(result) > 0 {
		if raw, err := json.Marshal(result); err == nil {
			_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), snapshotListCacheTTL).Err()
		}
	}

	log.Printf("[FilmHotList] 获取分类热播列表 field=%s id=%d count=%d offset=%d limit=%d cost=%s",
		field, id, len(result), offset, limit, time.Since(startedAt))
	return result
}

func GetSnapshotHotPoolByCategoryReadModel(version string, field string, id int64, poolSize int) []model.MovieBasicInfo {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	id = support.ResolveCategoryID(id)
	if version == "" || id <= 0 || poolSize <= 0 {
		return []model.MovieBasicInfo{}
	}

	cacheKey := fmt.Sprintf("EcoHub:snap_hot_pool:v%s:%s:%d:%d", version, field, id, poolSize)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached []model.MovieBasicInfo
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached
			}
		}
	}

	query := db.Mdb.Model(&model.FilmListSnapshot{}).
		Select(basicSelectFields).
		Where("snapshot_version = ?", version)
	if field == "pid" {
		query = query.Where("pid = ?", id)
	} else {
		query = query.Where("cid = ?", id)
	}

	var snapshots []model.FilmListSnapshot
	_ = query.Order("hits DESC, id DESC").Limit(poolSize).Find(&snapshots).Error
	result := BuildMovieBasicInfosFromSnapshots(snapshots...)

	if db.Rdb != nil && len(result) > 0 {
		if raw, err := json.Marshal(result); err == nil {
			_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), snapshotListCacheTTL).Err()
		}
	}
	return result
}

func GetSnapshotDynamicHotMovieListByCategoryReadModel(version string, field string, id int64, limit int, poolSize int) []model.MovieBasicInfo {
	if limit <= 0 {
		return []model.MovieBasicInfo{}
	}
	if poolSize <= 0 {
		poolSize = 50
	}
	pool := GetSnapshotHotPoolByCategoryReadModel(version, field, id, poolSize)
	if len(pool) <= limit {
		res := make([]model.MovieBasicInfo, len(pool))
		copy(res, pool)
		return res
	}

	indices := make([]int, len(pool))
	for i := range indices {
		indices[i] = i
	}
	rand.Shuffle(len(indices), func(i, j int) {
		indices[i], indices[j] = indices[j], indices[i]
	})

	result := make([]model.MovieBasicInfo, limit)
	for i := 0; i < limit; i++ {
		result[i] = pool[indices[i]]
	}
	return result
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

	cacheKey := fmt.Sprintf("EcoHub:snap_sort:v%s:%d:%d:p%d:s%d", version, pid, sortType, page.Current, page.PageSize)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var item categoryPageCacheItem
			if json.Unmarshal([]byte(data), &item) == nil {
				page.Total = item.Total
				page.PageCount = item.PageCount
				return item.Movies
			}
		}
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
	if err := query.Select(basicSelectFields).Order(orderClause).Offset(offset).Limit(page.PageSize).Find(&snapshots).Error; err != nil {
		return []model.MovieBasicInfo{}
	}
	result := BuildMovieBasicInfosFromSnapshots(snapshots...)

	if db.Rdb != nil && len(result) > 0 {
		item := categoryPageCacheItem{
			Total:     page.Total,
			PageCount: page.PageCount,
			Movies:    result,
		}
		if raw, err := json.Marshal(item); err == nil {
			_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), snapshotPageCacheTTL).Err()
		}
	}

	log.Printf("[FilmSortList] 获取分类排序列表 pid=%d sortType=%d total=%d page=%d size=%d cost=%s",
		pid, sortType, page.Total, page.Current, len(result), time.Since(startedAt))
	return result
}



