package snapshot

import (
	"log"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
)

func buildFilmListSnapshot(version string, index model.FilmIndex) model.FilmListSnapshot {
	return model.FilmListSnapshot{
		SnapshotVersion:    version,
		Mid:                index.Mid,
		SourceId:           index.FirstSourceId,
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
		UpdateReason:       index.UpdateReason,
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
		Writer:             index.Writer,
		Blurb:              index.Blurb,
		Content:            index.Content,
		ReleaseDate:        index.ReleaseDate,
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
	RemoveMidsFromActiveFilmSearchIndex(version, ids)
	invalidateDeletedSnapshotCaches(version, ids)
	RefreshAccessDataCaches()
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

func UpsertActiveSnapshotByMid(mid int64) error {
	_, _, err := UpsertActiveSnapshotsByMids(mid)
	return err
}

func UpsertActiveSnapshotsByMids(mids ...int64) (string, int, error) {
	activeSnapshotUpsertMu.Lock()
	defer activeSnapshotUpsertMu.Unlock()
	startedAt := time.Now()

	version := strings.TrimSpace(GetActiveReadModelVersion())
	if version == "" {
		version = EnsureLiveReadVersion()
		if err := LoadActiveFilmReadModel(version); err != nil {
			return "", 0, err
		}
	}

	ids := NormalizeSnapshotMIDs(mids)
	if len(ids) == 0 {
		return version, 0, nil
	}

	var existing []int64
	if err := db.Mdb.Model(&model.FilmIndex{}).Where("mid IN ?", ids).Pluck("mid", &existing).Error; err != nil {
		return "", 0, err
	}
	deletedMIDs := diffMIDs(ids, existing)
	if len(deletedMIDs) > 0 {
		RemoveMidsFromActiveFilmSearchIndex(version, deletedMIDs)
	}
	if len(existing) > 0 {
		UpsertMidsToActiveFilmSearchIndex(version, existing)
	}
	scheduleSnapshotCacheInvalidation(version, ids)
	log.Printf("[Snapshot] 列表可见性已跟随 film_index version=%s input=%d kept=%d deleted=%d cost=%s", version, len(ids), len(existing), len(deletedMIDs), time.Since(startedAt))
	return version, len(existing), nil
}

func NormalizeSnapshotMIDs(mids []int64) []int64 {
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
