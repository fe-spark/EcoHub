package portal

import (
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/taxonomy"
)

func currentProvideCategoryIDsBySourceKey(sourceKey string) ([]int64, error) {
	sourceKey = strings.TrimSpace(sourceKey)
	if sourceKey == "" {
		return nil, nil
	}

	sourceID, sourceTypeID, ok := taxonomy.ParseSourceCategoryKey(sourceKey)
	if !ok {
		return nil, nil
	}
	if localID := taxonomy.GetLocalCategoryId(sourceID, sourceTypeID); localID > 0 {
		return []int64{localID}, nil
	}
	if db.Mdb == nil {
		return nil, nil
	}

	var mappings []model.CategoryMapping
	if err := db.Mdb.Model(&model.CategoryMapping{}).
		Select("category_id").
		Where("source_id = ? AND source_type_id = ? AND category_id > 0", sourceID, sourceTypeID).
		Find(&mappings).Error; err != nil {
		return nil, err
	}

	ids := make([]int64, 0, len(mappings))
	seen := make(map[int64]struct{}, len(mappings))
	for _, mapping := range mappings {
		if _, exists := seen[mapping.CategoryId]; exists {
			continue
		}
		seen[mapping.CategoryId] = struct{}{}
		ids = append(ids, mapping.CategoryId)
	}
	return ids, nil
}

func resolveProvideCurrentCategoryIDFromSnapshot(snapshot model.FilmListSnapshot) (int64, error) {
	ids, err := currentProvideCategoryIDsBySourceKey(snapshot.CategoryKey)
	if err != nil {
		return 0, err
	}
	if len(ids) > 0 {
		return ids[0], nil
	}
	ids, err = currentProvideCategoryIDsBySourceKey(snapshot.RootCategoryKey)
	if err != nil {
		return 0, err
	}
	if len(ids) > 0 {
		return ids[0], nil
	}
	if snapshot.Cid > 0 {
		return taxonomy.ResolveCategoryID(snapshot.Cid), nil
	}
	if snapshot.Pid > 0 {
		return taxonomy.ResolveCategoryID(snapshot.Pid), nil
	}
	return 0, nil
}

func resolveProvideCurrentRootCategoryIDFromSnapshot(snapshot model.FilmListSnapshot) (int64, error) {
	ids, err := currentProvideCategoryIDsBySourceKey(snapshot.RootCategoryKey)
	if err != nil {
		return 0, err
	}
	if len(ids) > 0 {
		rootID := taxonomy.GetRootId(ids[0])
		if rootID > 0 {
			return rootID, nil
		}
		return ids[0], nil
	}
	categoryID, err := resolveProvideCurrentCategoryIDFromSnapshot(snapshot)
	if err != nil {
		return 0, err
	}
	if categoryID > 0 {
		rootID := taxonomy.GetRootId(categoryID)
		if rootID > 0 {
			return rootID, nil
		}
		return categoryID, nil
	}
	return 0, nil
}

func resolveProvideTypeFromSnapshot(snapshot model.FilmListSnapshot) (int64, string, error) {
	categoryID, err := resolveProvideCurrentCategoryIDFromSnapshot(snapshot)
	if err != nil {
		return 0, "", err
	}
	if categoryID > 0 {
		if name := taxonomy.GetCategoryNameById(categoryID); name != "" {
			return categoryID, name, nil
		}
		if name := taxonomy.GetMainCategoryName(categoryID); name != "" {
			return categoryID, name, nil
		}
	}
	if snapshot.Cid > 0 {
		if name := taxonomy.GetCategoryNameById(snapshot.Cid); name != "" {
			return snapshot.Cid, name, nil
		}
		return snapshot.Cid, snapshot.CName, nil
	}
	if snapshot.Pid > 0 {
		if name := taxonomy.GetMainCategoryName(snapshot.Pid); name != "" {
			return snapshot.Pid, name, nil
		}
		return snapshot.Pid, snapshot.CName, nil
	}
	return 0, snapshot.CName, nil
}

func resolveProvideSnapshotPlayFromSummary(snapshot model.FilmListSnapshot) string {
	return strings.TrimSpace(snapshot.PlayFromSummary)
}

func resolveProvideSnapshotVodTime(snapshot model.FilmListSnapshot) string {
	stamp := snapshot.CollectStamp
	if stamp <= 0 {
		stamp = snapshot.UpdateStamp
	}
	if stamp <= 0 {
		return ""
	}
	return time.Unix(stamp, 0).Format("2006-01-02 15:04:05")
}
