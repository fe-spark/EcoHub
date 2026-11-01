package service

import (
	"strings"
	"time"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/support"
)

func currentProvideCategoryIDsBySourceKey(sourceKey string) ([]int64, error) {
	sourceKey = strings.TrimSpace(sourceKey)
	if sourceKey == "" {
		return nil, nil
	}

	mappings, err := repository.ListCategoryMappings()
	if err != nil {
		return nil, err
	}

	ids := make([]int64, 0)
	seen := make(map[int64]struct{})
	for _, mapping := range mappings {
		if support.BuildSourceCategoryKey(mapping.SourceId, mapping.SourceTypeId) != sourceKey || mapping.CategoryId <= 0 {
			continue
		}
		if _, ok := seen[mapping.CategoryId]; ok {
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
		return repository.ResolveCategoryID(snapshot.Cid), nil
	}
	if snapshot.Pid > 0 {
		return repository.ResolveCategoryID(snapshot.Pid), nil
	}
	return 0, nil
}

func resolveProvideCurrentRootCategoryIDFromSnapshot(snapshot model.FilmListSnapshot) (int64, error) {
	ids, err := currentProvideCategoryIDsBySourceKey(snapshot.RootCategoryKey)
	if err != nil {
		return 0, err
	}
	if len(ids) > 0 {
		rootID := repository.GetRootId(ids[0])
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
		rootID := repository.GetRootId(categoryID)
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
		if name := support.GetCategoryNameById(categoryID); name != "" {
			return categoryID, name, nil
		}
		if name := support.GetMainCategoryName(categoryID); name != "" {
			return categoryID, name, nil
		}
	}
	if snapshot.Cid > 0 {
		if name := support.GetCategoryNameById(snapshot.Cid); name != "" {
			return snapshot.Cid, name, nil
		}
		return snapshot.Cid, snapshot.CName, nil
	}
	if snapshot.Pid > 0 {
		if name := support.GetMainCategoryName(snapshot.Pid); name != "" {
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
