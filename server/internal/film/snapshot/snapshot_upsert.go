package snapshot

import (
	"strings"

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

// InvalidateCachesAfterSourceDelete 删站后的缓存与检索索引清理：
// 内存索引属于被删站或主站已变时直接整盘丢掉，跳过十万级 mid 遍历与逐 key Redis Pipeline 删除。
func InvalidateCachesAfterSourceDelete(deletedSourceID string) {
	deletedSourceID = strings.TrimSpace(deletedSourceID)
	cur := activeFilmSearchMetas.Load()
	if cur != nil && (cur.PrimaryID == deletedSourceID || cur.PrimaryID != currentPrimarySourceID()) {
		DropPrimarySearchIndex()
	}
	version := GetActiveSnapshotVersion()
	if version != "" {
		applyBroadSnapshotCacheInvalidation(version, nil)
	} else {
		RefreshAccessDataCaches()
	}
	ClearDynamicPlayCaches()
}

func UpsertActiveSnapshotsByMids(mids ...int64) (string, int, error) {
	return upsertActiveSnapshots(mids, true)
}

// TouchActiveSnapshotsByMids 只登记列表缓存失效。不碰检索索引。
func TouchActiveSnapshotsByMids(mids ...int64) (string, int, error) {
	return upsertActiveSnapshots(mids, false)
}

func upsertActiveSnapshots(mids []int64, refreshSearch bool) (string, int, error) {
	activeSnapshotUpsertMu.Lock()
	defer activeSnapshotUpsertMu.Unlock()

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

	var rows []struct {
		Mid           int64
		FirstSourceId string
	}
	if err := db.Mdb.Model(&model.FilmIndex{}).
		Select("mid, first_source_id").
		Where("mid IN ?", ids).
		Find(&rows).Error; err != nil {
		return "", 0, err
	}
	primary := currentPrimarySourceID()
	existing := make([]int64, 0, len(rows))
	primaryMids := make([]int64, 0, len(rows))
	otherMids := make([]int64, 0, len(rows))
	for _, row := range rows {
		existing = append(existing, row.Mid)
		src := strings.TrimSpace(row.FirstSourceId)
		if primary != "" && src == primary {
			primaryMids = append(primaryMids, row.Mid)
			continue
		}
		otherMids = append(otherMids, row.Mid)
	}
	deletedMIDs := diffMIDs(ids, existing)
	if refreshSearch {
		remove := indexedMIDs(append(append([]int64{}, deletedMIDs...), otherMids...))
		if len(remove) > 0 {
			RemoveMidsFromActiveFilmSearchIndex(version, remove)
		}
		if len(primaryMids) > 0 {
			UpsertMidsToActiveFilmSearchIndex(version, primaryMids)
		}
	}
	scheduleSnapshotCacheInvalidation(version, ids)
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
