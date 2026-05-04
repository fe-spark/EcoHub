package film

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	snapshotBuildBatchSize = 1000
	snapshotRetainVersions = 2
)

func GetActiveSnapshotVersion() string {
	version, err := db.Rdb.Get(db.Cxt, config.SnapshotActiveVersionKey).Result()
	if err == nil && strings.TrimSpace(version) != "" {
		return strings.TrimSpace(version)
	}

	var latest model.FilmListSnapshot
	if err := db.Mdb.Select("snapshot_version").Order("id DESC").First(&latest).Error; err == nil && latest.SnapshotVersion != "" {
		if err := SetActiveSnapshotVersion(latest.SnapshotVersion); err != nil {
			log.Printf("SetActiveSnapshotVersion Error: %v", err)
		}
		return latest.SnapshotVersion
	}
	return ""
}

func SetActiveSnapshotVersion(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil
	}
	return db.Rdb.Set(db.Cxt, config.SnapshotActiveVersionKey, version, 0).Err()
}

func NewSnapshotVersion() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func RebuildFilmListSnapshot(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		version = NewSnapshotVersion()
	}

	startedAt := time.Now()
	return db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("snapshot_version = ?", version).Unscoped().Delete(&model.FilmListSnapshot{}).Error; err != nil {
			return err
		}

		var lastID uint
		total := 0
		for {
			batchStartedAt := time.Now()
			var indexes []model.FilmIndex
			if err := tx.Joins("JOIN "+model.TableMovieDetail+" ON "+model.TableMovieDetail+".mid = film_index.mid AND "+model.TableMovieDetail+".deleted_at IS NULL").
				Where("film_index.id > ?", lastID).
				Order("film_index.id ASC").
				Limit(snapshotBuildBatchSize).
				Find(&indexes).Error; err != nil {
				return err
			}
			if len(indexes) == 0 {
				break
			}

			snapshots := make([]model.FilmListSnapshot, 0, len(indexes))
			for _, index := range indexes {
				snapshots = append(snapshots, buildFilmListSnapshot(version, index))
				lastID = index.ID
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(snapshots, snapshotBuildBatchSize).Error; err != nil {
				return err
			}
			total += len(snapshots)
			log.Printf(
				"[Snapshot] 构建进度 version=%s total=%d batch=%d last_id=%d cost=%s total_cost=%s",
				version,
				total,
				len(snapshots),
				lastID,
				time.Since(batchStartedAt),
				time.Since(startedAt),
			)
		}

		return nil
	})
}

func ActivateRebuiltFilmListSnapshot(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		version = NewSnapshotVersion()
	}
	if err := RebuildFilmListSnapshot(version); err != nil {
		return err
	}
	if err := SetActiveSnapshotVersion(version); err != nil {
		return err
	}
	if err := db.Rdb.Set(db.Cxt, config.SnapshotBuildVersionKey, version, 0).Err(); err != nil {
		log.Printf("Set SnapshotBuildVersion Error: %v", err)
	}
	RefreshAccessDataCaches()
	ClearAdminFilmSearchCache()
	WarmSnapshotFilterCaches(version)
	pruneOldFilmListSnapshots(snapshotRetainVersions)
	return nil
}

func EnsureActiveFilmListSnapshot() error {
	if GetActiveSnapshotVersion() != "" {
		return nil
	}

	var count int64
	if err := db.Mdb.Model(&model.FilmIndex{}).
		Joins("JOIN " + model.TableMovieDetail + " ON " + model.TableMovieDetail + ".mid = film_index.mid AND " + model.TableMovieDetail + ".deleted_at IS NULL").
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return nil
	}

	version := NewSnapshotVersion()
	if err := ActivateRebuiltFilmListSnapshot(version); err != nil {
		return err
	}
	log.Printf("[Snapshot] 已基于现有影片数据构建首个前台快照, version=%s, film_count=%d", version, count)
	return nil
}

func pruneOldFilmListSnapshots(retain int) {
	if retain <= 0 {
		retain = 1
	}

	var versions []string
	if err := db.Mdb.Model(&model.FilmListSnapshot{}).
		Select("snapshot_version").
		Group("snapshot_version").
		Order("MAX(id) DESC").
		Limit(retain).
		Pluck("snapshot_version", &versions).Error; err != nil {
		log.Printf("pruneOldFilmListSnapshots Versions Error: %v", err)
		return
	}
	if len(versions) == 0 {
		return
	}

	if err := db.Mdb.Where("snapshot_version NOT IN ?", versions).Unscoped().Delete(&model.FilmListSnapshot{}).Error; err != nil {
		log.Printf("pruneOldFilmListSnapshots Delete Error: %v", err)
	}
}

func buildFilmListSnapshot(version string, index model.FilmIndex) model.FilmListSnapshot {
	return model.FilmListSnapshot{
		SnapshotVersion:  version,
		Mid:              index.Mid,
		ContentKey:       index.ContentKey,
		SourceId:         index.SourceId,
		DbId:             index.DbId,
		Cid:              index.Cid,
		Pid:              index.Pid,
		RootCategoryKey:  index.RootCategoryKey,
		CategoryKey:      index.CategoryKey,
		OriginalCategory: index.OriginalCategory,
		CName:            index.CName,
		SeriesKey:        index.SeriesKey,
		Name:             index.Name,
		SubTitle:         index.SubTitle,
		ClassTag:         index.ClassTag,
		Area:             index.Area,
		Language:         index.Language,
		Year:             index.Year,
		Initial:          index.Initial,
		Score:            index.Score,
		UpdateStamp:      index.UpdateStamp,
		Hits:             index.Hits,
		State:            index.State,
		Remarks:          index.Remarks,
		Picture:          index.Picture,
		PictureSlide:     index.PictureSlide,
		Actor:            index.Actor,
		Director:         index.Director,
		Blurb:            index.Blurb,
		CollectStamp:     index.CollectStamp,
		CategoryVersion:  index.CategoryVersion,
		RuleVersion:      index.RuleVersion,
		PlayFromSummary:  index.PlayFromSummary,
	}
}

func DeleteActiveSnapshotsByMids(mids ...int64) {
	version := GetActiveSnapshotVersion()
	if version == "" || len(mids) == 0 {
		return
	}
	ids := make([]int64, 0, len(mids))
	seen := make(map[int64]struct{}, len(mids))
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		seen[mid] = struct{}{}
		ids = append(ids, mid)
	}
	if len(ids) == 0 {
		return
	}
	result := db.Mdb.Unscoped().Where("snapshot_version = ? AND mid IN ?", version, ids).Delete(&model.FilmListSnapshot{})
	if result.Error != nil {
		log.Printf("DeleteActiveSnapshotsByMids Error: %v", result.Error)
		return
	}
	if result.RowsAffected > 0 {
		RefreshAccessDataCaches()
	}
}

func DeleteActiveSnapshotsByCategory(field string, id int64) {
	version := GetActiveSnapshotVersion()
	if version == "" || id <= 0 {
		return
	}
	query := applyCategoryFieldFilter(db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", version), field, id)
	result := query.Unscoped().Delete(&model.FilmListSnapshot{})
	if result.Error != nil {
		log.Printf("DeleteActiveSnapshotsByCategory Error: %v", result.Error)
		return
	}
	if result.RowsAffected > 0 {
		RefreshAccessDataCaches()
	}
}

func DeleteActiveRootSnapshots(pid int64) {
	version := GetActiveSnapshotVersion()
	if version == "" || pid <= 0 {
		return
	}
	result := db.Mdb.Unscoped().
		Where("snapshot_version = ? AND (cid = ? OR (pid = ? AND cid = 0))", version, pid, pid).
		Delete(&model.FilmListSnapshot{})
	if result.Error != nil {
		log.Printf("DeleteActiveRootSnapshots Error: %v", result.Error)
		return
	}
	if result.RowsAffected > 0 {
		RefreshAccessDataCaches()
	}
}

func RestoreActiveSnapshotsByCategory(cid int64) {
	version := GetActiveSnapshotVersion()
	if version == "" || cid <= 0 {
		return
	}
	var indexes []model.FilmIndex
	if err := db.Mdb.Where("cid = ?", cid).Find(&indexes).Error; err != nil {
		log.Printf("RestoreActiveSnapshotsByCategory Query Error: %v", err)
		return
	}
	if len(indexes) == 0 {
		return
	}
	mids := make([]int64, 0, len(indexes))
	snapshots := make([]model.FilmListSnapshot, 0, len(indexes))
	for _, index := range indexes {
		mids = append(mids, index.Mid)
		snapshots = append(snapshots, buildFilmListSnapshot(version, index))
	}
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("snapshot_version = ? AND mid IN ?", version, mids).Delete(&model.FilmListSnapshot{}).Error; err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(snapshots, snapshotBuildBatchSize).Error
	})
	if err != nil {
		log.Printf("RestoreActiveSnapshotsByCategory Error: %v", err)
		return
	}
	RefreshAccessDataCaches()
}

func BuildSnapshotQueryByTags(version string, st model.SearchTagsVO) *gorm.DB {
	query := db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", strings.TrimSpace(version))
	return BuildFilmIndexQueryByTags(query, st)
}

func GetSnapshotByMid(version string, mid int64) *model.FilmListSnapshot {
	version = strings.TrimSpace(version)
	if version == "" || mid <= 0 {
		return nil
	}
	var snapshot model.FilmListSnapshot
	if err := db.Mdb.Where("snapshot_version = ? AND mid = ?", version, mid).First(&snapshot).Error; err != nil {
		return nil
	}
	return &snapshot
}

func GetMovieDetailBySnapshot(snapshot model.FilmListSnapshot) *model.MovieDetail {
	if snapshot.Mid <= 0 {
		return nil
	}
	var movieDetailInfo model.MovieDetailInfo
	if err := db.Mdb.Where("mid = ?", snapshot.Mid).First(&movieDetailInfo).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("GetMovieDetailBySnapshot Error: %v", err)
		}
		return nil
	}
	var detail model.MovieDetail
	if err := json.Unmarshal([]byte(movieDetailInfo.Content), &detail); err != nil {
		log.Printf("Unmarshal Snapshot MovieDetail Error: %v", err)
		return nil
	}
	ApplyFilmListSnapshot(&detail, snapshot)
	normalizeMovieDetailLists(&detail)
	return &detail
}

func normalizeMovieDetailLists(detail *model.MovieDetail) {
	if detail == nil {
		return
	}
	if detail.PlayFrom == nil {
		detail.PlayFrom = []string{}
	}
	if detail.PlayList == nil {
		detail.PlayList = [][]model.MovieUrlInfo{}
	} else {
		for i, inner := range detail.PlayList {
			if inner == nil {
				detail.PlayList[i] = []model.MovieUrlInfo{}
			}
		}
	}
	if detail.DownloadList == nil {
		detail.DownloadList = [][]model.MovieUrlInfo{}
	} else {
		for i, inner := range detail.DownloadList {
			if inner == nil {
				detail.DownloadList[i] = []model.MovieUrlInfo{}
			}
		}
	}
}

func ListFilmSnapshotsByTags(version string, st model.SearchTagsVO, page *dto.Page) []model.FilmListSnapshot {
	page = ensurePage(page)
	qw := BuildSnapshotQueryByTags(version, st)

	dto.GetPage(qw, page)
	var snapshots []model.FilmListSnapshot
	if err := qw.Limit(page.PageSize).Offset(getPageOffset(page)).Find(&snapshots).Error; err != nil {
		log.Printf("ListFilmSnapshotsByTags Error: %v", err)
		return nil
	}
	return snapshots
}

func SearchSnapshotsByKeyword(version string, keyword string, page *dto.Page) []model.FilmListSnapshot {
	page = ensurePage(page)
	version = strings.TrimSpace(version)
	keywordQuery := buildNameKeywordQuery(keyword)
	var snapshots []model.FilmListSnapshot
	query := applyVisibleCategoryFilter(db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", version)).
		Where(keywordQuery).
		Order("year DESC, " + latestUpdateOrderSQL)

	dto.GetPage(query, page)
	if err := query.Limit(page.PageSize).Offset(getPageOffset(page)).Find(&snapshots).Error; err != nil {
		log.Printf("SearchSnapshotsByKeyword Error: %v", err)
		return nil
	}
	return snapshots
}

func GetSnapshotMovieListByCategory(version string, field string, id int64, limit int, offset int) []model.MovieBasicInfo {
	var snapshots []model.FilmListSnapshot
	query := applyCategoryFieldFilter(db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", strings.TrimSpace(version)), field, id)
	if err := query.Order(latestUpdateOrderSQL).Limit(limit).Offset(offset).Find(&snapshots).Error; err != nil {
		log.Printf("GetSnapshotMovieListByCategory Error: %v", err)
		return nil
	}
	return BuildMovieBasicInfosFromSnapshots(snapshots...)
}

func GetSnapshotMovieListByCategoryPage(version string, field string, id int64, page *dto.Page) []model.MovieBasicInfo {
	page = ensurePage(page)
	var snapshots []model.FilmListSnapshot
	query := applyCategoryFieldFilter(db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", strings.TrimSpace(version)), field, id)
	dto.GetPage(query, page)
	if err := query.Order(latestUpdateOrderSQL).Limit(page.PageSize).Offset(getPageOffset(page)).Find(&snapshots).Error; err != nil {
		log.Printf("GetSnapshotMovieListByCategoryPage Error: %v", err)
		return nil
	}
	return BuildMovieBasicInfosFromSnapshots(snapshots...)
}

func GetSnapshotHotMovieListByCategory(version string, field string, id int64, limit int, offset int) []model.MovieBasicInfo {
	var snapshots []model.FilmListSnapshot
	hotSince := time.Now().AddDate(0, -1, 0).Unix()
	query := applyCategoryFieldFilter(db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", strings.TrimSpace(version)), field, id)
	if err := query.Where("update_stamp > ?", hotSince).
		Order("year DESC, hits DESC, mid DESC").
		Limit(limit).
		Offset(offset).
		Find(&snapshots).Error; err != nil {
		log.Printf("GetSnapshotHotMovieListByCategory Error: %v", err)
		return nil
	}
	return BuildMovieBasicInfosFromSnapshots(snapshots...)
}

func GetSnapshotMovieListBySort(version string, sortType int, pid int64, page *dto.Page) []model.MovieBasicInfo {
	page = ensurePage(page)
	var snapshots []model.FilmListSnapshot
	query := applyMovieSortQuery(applyCategoryFieldFilter(db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", version), "pid", pid), sortType)
	if err := query.Limit(page.PageSize).Offset(getPageOffset(page)).Find(&snapshots).Error; err != nil {
		log.Printf("GetSnapshotMovieListBySort Error: %v", err)
		return nil
	}
	return BuildMovieBasicInfosFromSnapshots(snapshots...)
}

func SnapshotClassifyCacheKey(version string, pid int64, page *dto.Page) string {
	page = ensurePage(page)
	return fmt.Sprintf("%s:v%s:P%d:C%d:S%d", config.FilmClassifyCacheKey, version, pid, page.Current, page.PageSize)
}

func SnapshotSearchCacheKey(version string, st model.SearchTagsVO, page *dto.Page) string {
	page = ensurePage(page)
	st = normalizeSearchTagsVO(st)
	payload := struct {
		Version string
		Tags    model.SearchTagsVO
		Page    dto.Page
	}{Version: version, Tags: st, Page: *page}
	data, _ := json.Marshal(payload)
	return fmt.Sprintf("%s:%x", config.FilmClassifySearchKey, data)
}

func RefreshAccessDataCaches() {
	db.Rdb.Del(
		db.Cxt,
		config.ActiveCategoryTreeKey,
		config.CategoryTreeKey,
		config.TVBoxConfigCacheKey,
		config.BannersKey,
	)
	bumpSearchTagsCacheVersion()
	clearCachePatterns(
		fmt.Sprintf("%s*", config.IndexPageCacheKey),
		fmt.Sprintf("%s:*", config.TVBoxList),
		fmt.Sprintf("%s:*", config.TVBoxNetworkConfigCacheKey),
		fmt.Sprintf("%s:*", config.FilmClassifyCacheKey),
		fmt.Sprintf("%s:*", config.FilmClassifySearchKey),
		fmt.Sprintf("%s:*", config.SearchTags),
	)
}

func ClearSnapshotState() {
	db.Rdb.Del(db.Cxt, config.SnapshotActiveVersionKey, config.SnapshotBuildVersionKey)
	RefreshAccessDataCaches()
}

func clearCachePatterns(patterns ...string) {
	for _, pattern := range patterns {
		iter := db.Rdb.Scan(db.Cxt, 0, pattern, config.MaxScanCount).Iterator()
		for iter.Next(db.Cxt) {
			if err := db.Rdb.Del(db.Cxt, iter.Val()).Err(); err != nil {
				log.Printf("clearCachePatterns Del Error: key=%s err=%v", iter.Val(), err)
			}
		}
		if err := iter.Err(); err != nil {
			log.Printf("clearCachePatterns Scan Error: pattern=%s err=%v", pattern, err)
		}
	}
}
