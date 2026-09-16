package film

import (
	"log"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func buildFilmListSnapshot(version string, index model.FilmIndex) model.FilmListSnapshot {
	return model.FilmListSnapshot{
		SnapshotVersion:    version,
		Mid:                index.Mid,
		ContentKey:         index.ContentKey,
		SourceId:           index.SourceId,
		DbId:               index.DbId,
		Cid:                index.Cid,
		Pid:                index.Pid,
		RootCategoryKey:    index.RootCategoryKey,
		CategoryKey:        index.CategoryKey,
		OriginalCategory:   index.OriginalCategory,
		CName:              index.CName,
		SeriesKey:          index.SeriesKey,
		Name:               index.Name,
		SubTitle:           index.SubTitle,
		ClassTag:           index.ClassTag,
		Area:               index.Area,
		Language:           index.Language,
		Year:               index.Year,
		Initial:            index.Initial,
		Score:              index.Score,
		UpdateStamp:        index.UpdateStamp,
		Hits:               index.Hits,
		State:              index.State,
		Remarks:            index.Remarks,
		Picture:            index.Picture,
		PictureSlide:       index.PictureSlide,
		CustomPicture:      index.CustomPicture,
		CustomPictureSlide: index.CustomPictureSlide,
		IsCustomPicture:    index.IsCustomPicture,
		Actor:              index.Actor,
		Director:           index.Director,
		Blurb:              index.Blurb,
		CollectStamp:       index.CollectStamp,
		CategoryVersion:    index.CategoryVersion,
		RuleVersion:        index.RuleVersion,
		PlayFromSummary:    index.PlayFromSummary,
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
	RemoveMidsFromActiveFilmSearchIndex(version, ids)
	invalidateDeletedSnapshotCaches(version, ids)
	RefreshAccessDataCaches()
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
		BumpSearchCacheVersion()
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
		BumpSearchCacheVersion()
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
	BumpSearchCacheVersion()
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

	updatedCount := 0
	deletedCount := 0
	processed := 0
	allKeptMIDs := make([]int64, 0, len(ids))
	allDeletedMIDs := make([]int64, 0)
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
			writeCost := time.Since(writeStartedAt)

			updatedCount += len(batchSnapshots)
			deletedCount += len(deletedMIDs)
			processed += len(batchIDs)
			allKeptMIDs = append(allKeptMIDs, keptMIDs...)
			allDeletedMIDs = append(allDeletedMIDs, deletedMIDs...)
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
	if len(allDeletedMIDs) > 0 {
		RemoveMidsFromActiveFilmSearchIndex(version, allDeletedMIDs)
	}
	if len(allKeptMIDs) > 0 {
		UpsertMidsToActiveFilmSearchIndex(version, allKeptMIDs)
	}
	invalidateSnapshotDataCaches(version, ids)
	applyCost := time.Since(applyStartedAt)
	RefreshAccessDataCaches()
	ClearAdminFilmSearchCache()
	log.Printf("[Snapshot] 快速增量发布完成 version=%s input=%d updated=%d deleted=%d apply=%s total_cost=%s", version, len(ids), updatedCount, deletedCount, applyCost, time.Since(startedAt))
	return version, updatedCount, nil
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
