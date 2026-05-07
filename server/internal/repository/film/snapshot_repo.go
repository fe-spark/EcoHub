package film

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
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

var activeSnapshotUpsertMu sync.Mutex

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

func GetActiveReadModelVersion() string {
	readModel := GetActiveFilmReadModel()
	if readModel == nil {
		return GetActiveSnapshotVersion()
	}
	return readModel.Version
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
	if err := LoadActiveFilmReadModel(version); err != nil {
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

func pruneOldFilterOptionSnapshots(retain int) {
	if retain <= 0 {
		retain = 1
	}

	var versions []string
	if err := db.Mdb.Model(&model.FilmFilterOptionSnapshot{}).
		Select("snapshot_version").
		Group("snapshot_version").
		Order("MAX(id) DESC").
		Limit(retain).
		Pluck("snapshot_version", &versions).Error; err != nil {
		log.Printf("pruneOldFilterOptionSnapshots Versions Error: %v", err)
		return
	}
	if len(versions) == 0 {
		return
	}

	if err := db.Mdb.Where("snapshot_version NOT IN ?", versions).Unscoped().Delete(&model.FilmFilterOptionSnapshot{}).Error; err != nil {
		log.Printf("pruneOldFilterOptionSnapshots Delete Error: %v", err)
	}
}

func pruneOldFilterIndexSnapshots(retain int) {
	if retain <= 0 {
		retain = 1
	}

	var versions []string
	if err := db.Mdb.Model(&model.FilmFilterIndexSnapshot{}).
		Select("snapshot_version").
		Group("snapshot_version").
		Order("MAX(id) DESC").
		Limit(retain).
		Pluck("snapshot_version", &versions).Error; err != nil {
		log.Printf("pruneOldFilterIndexSnapshots Versions Error: %v", err)
		return
	}
	if len(versions) == 0 {
		return
	}

	if err := db.Mdb.Where("snapshot_version NOT IN ?", versions).Unscoped().Delete(&model.FilmFilterIndexSnapshot{}).Error; err != nil {
		log.Printf("pruneOldFilterIndexSnapshots Delete Error: %v", err)
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
		rebuildActiveFilterOptions(version)
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
		rebuildActiveFilterOptions(version)
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
		rebuildActiveFilterOptions(version)
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
	rebuildActiveFilterOptions(version)
}

func rebuildActiveFilterOptions(version string) {
	if err := LoadActiveFilmReadModel(version); err != nil {
		log.Printf("LoadActiveFilmReadModel Error: %v", err)
	}
}

func RefreshActiveReadModelArtifacts() error {
	version := GetActiveReadModelVersion()
	if strings.TrimSpace(version) == "" {
		return nil
	}
	if err := LoadActiveFilmReadModel(version); err != nil {
		return err
	}
	return nil
}

func RefreshActiveSnapshotReadModel() error {
	return RefreshActiveReadModelArtifacts()
}

func UpsertActiveSnapshotByMid(mid int64) error {
	_, _, err := UpsertActiveSnapshotsByMids(mid)
	return err
}

func UpsertActiveSnapshotsByMids(mids ...int64) (string, int, error) {
	activeSnapshotUpsertMu.Lock()
	defer activeSnapshotUpsertMu.Unlock()
	startedAt := time.Now()

	version := GetActiveReadModelVersion()
	if strings.TrimSpace(version) == "" {
		version = GetActiveSnapshotVersion()
	}
	if strings.TrimSpace(version) == "" {
		version = NewSnapshotVersion()
		if err := ActivateRebuiltFilmListSnapshot(version); err != nil {
			return "", 0, err
		}
		return version, 0, nil
	}

	ids := normalizeSnapshotMIDs(mids)
	if len(ids) == 0 {
		return version, 0, nil
	}

	allSnapshots := make([]model.FilmListSnapshot, 0, len(ids))
	allDeletedMIDs := make([]int64, 0)
	processed := 0
	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		for _, batchIDs := range chunkSnapshotMIDs(ids, snapshotBuildBatchSize) {
			batchStartedAt := time.Now()

			var indexes []model.FilmIndex
			queryStartedAt := time.Now()
			if err := tx.Joins("JOIN "+model.TableMovieDetail+" ON "+model.TableMovieDetail+".mid = film_index.mid AND "+model.TableMovieDetail+".deleted_at IS NULL").
				Where("film_index.mid IN ?", batchIDs).
				Find(&indexes).Error; err != nil {
				return err
			}
			queryCost := time.Since(queryStartedAt)

			buildStartedAt := time.Now()
			batchSnapshots := make([]model.FilmListSnapshot, 0, len(indexes))
			keptMIDs := make([]int64, 0, len(indexes))
			for _, index := range indexes {
				if index.Mid <= 0 {
					continue
				}
				batchSnapshots = append(batchSnapshots, buildFilmListSnapshot(version, index))
				keptMIDs = append(keptMIDs, index.Mid)
			}
			deletedMIDs := diffMIDs(batchIDs, keptMIDs)
			buildCost := time.Since(buildStartedAt)

			writeStartedAt := time.Now()
			if err := tx.Unscoped().Where("snapshot_version = ? AND mid IN ?", version, batchIDs).Delete(&model.FilmListSnapshot{}).Error; err != nil {
				return err
			}
			if len(batchSnapshots) > 0 {
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(batchSnapshots, snapshotBuildBatchSize).Error; err != nil {
					return err
				}
			}
			if err := ReplaceFilterIndexSnapshotsTx(tx, version, batchSnapshots, batchIDs); err != nil {
				return err
			}
			writeCost := time.Since(writeStartedAt)

			allSnapshots = append(allSnapshots, batchSnapshots...)
			allDeletedMIDs = append(allDeletedMIDs, deletedMIDs...)
			processed += len(batchIDs)
			log.Printf(
				"[Snapshot] 快速增量发布进度 version=%s mid=%d/%d batch=%d updated=%d deleted=%d query=%s build=%s write=%s cost=%s total_cost=%s",
				version,
				processed,
				len(ids),
				len(batchIDs),
				len(batchSnapshots),
				len(deletedMIDs),
				queryCost,
				buildCost,
				writeCost,
				time.Since(batchStartedAt),
				time.Since(startedAt),
			)
		}
		return nil
	}); err != nil {
		return "", 0, err
	}

	applyStartedAt := time.Now()
	if err := ApplyActiveFilmReadModelSnapshots(version, allSnapshots, allDeletedMIDs); err != nil {
		return "", 0, err
	}
	applyCost := time.Since(applyStartedAt)
	RefreshAccessDataCaches()
	ClearAdminFilmSearchCache()
	log.Printf("[Snapshot] 快速增量发布完成 version=%s input=%d updated=%d deleted=%d apply=%s total_cost=%s", version, len(ids), len(allSnapshots), len(allDeletedMIDs), applyCost, time.Since(startedAt))
	return version, len(allSnapshots), nil
}

func chunkSnapshotMIDs(ids []int64, size int) [][]int64 {
	if len(ids) == 0 {
		return nil
	}
	if size <= 0 {
		size = snapshotBuildBatchSize
	}
	chunks := make([][]int64, 0, (len(ids)+size-1)/size)
	for start := 0; start < len(ids); start += size {
		end := start + size
		if end > len(ids) {
			end = len(ids)
		}
		chunks = append(chunks, ids[start:end])
	}
	return chunks
}

func normalizeSnapshotMIDs(mids []int64) []int64 {
	if len(mids) == 0 {
		return nil
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
	return ids
}

func diffMIDs(all []int64, kept []int64) []int64 {
	if len(all) == 0 {
		return nil
	}
	keptSet := make(map[int64]struct{}, len(kept))
	for _, mid := range kept {
		if mid > 0 {
			keptSet[mid] = struct{}{}
		}
	}
	deleted := make([]int64, 0)
	for _, mid := range all {
		if mid <= 0 {
			continue
		}
		if _, ok := keptSet[mid]; !ok {
			deleted = append(deleted, mid)
		}
	}
	return deleted
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

func GetMovieDetailBySnapshot(snapshot model.FilmListSnapshot) (*model.MovieDetail, int64) {
	if snapshot.Mid <= 0 {
		return nil, 0
	}
	var movieDetailInfo model.MovieDetailInfo
	if err := db.Mdb.Where("mid = ?", snapshot.Mid).First(&movieDetailInfo).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("GetMovieDetailBySnapshot Error: %v", err)
		}
		return nil, 0
	}
	var detail model.MovieDetail
	if err := json.Unmarshal([]byte(movieDetailInfo.Content), &detail); err != nil {
		log.Printf("Unmarshal Snapshot MovieDetail Error: %v", err)
		return nil, 0
	}
	ApplyFilmListSnapshot(&detail, snapshot)
	normalizeMovieDetailLists(&detail)
	return &detail, snapshot.UpdateStamp
}

func HasMovieDetail(mid int64) bool {
	if mid <= 0 {
		return false
	}
	var count int64
	if err := db.Mdb.Model(&model.MovieDetailInfo{}).Where("mid = ?", mid).Limit(1).Count(&count).Error; err != nil {
		log.Printf("HasMovieDetail Error: %v", err)
		return false
	}
	return count > 0
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

func GetSnapshotMovieListByCategory(version string, field string, id int64, limit int, offset int) []model.MovieBasicInfo {
	return GetSnapshotMovieListByCategoryReadModel(version, field, id, limit, offset)
}

func GetSnapshotMovieListByCategoryPage(version string, field string, id int64, page *dto.Page) []model.MovieBasicInfo {
	return GetSnapshotMovieListByCategoryPageReadModel(version, field, id, page)
}

func GetSnapshotHotMovieListByCategory(version string, field string, id int64, limit int, offset int) []model.MovieBasicInfo {
	return GetSnapshotHotMovieListByCategoryReadModel(version, field, id, limit, offset)
}

func GetSnapshotMovieListBySort(version string, sortType int, pid int64, page *dto.Page) []model.MovieBasicInfo {
	return GetSnapshotMovieListBySortReadModel(version, sortType, pid, page)
}

func SnapshotClassifyCacheKey(version string, pid int64, page *dto.Page) string {
	page = ensurePage(page)
	return fmt.Sprintf("%s:v%s:P%d:C%d:S%d", config.FilmClassifyCacheKey, version, pid, page.Current, page.PageSize)
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
		fmt.Sprintf("%s:*", config.SearchTags),
	)
}

func ClearSnapshotState() {
	ClearActiveFilmReadModel()
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
