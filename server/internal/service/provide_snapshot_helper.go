package service

import (
	"log"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/support"
)

func currentProvideCategoryIDsBySourceKey(sourceKey string) []int64 {
	sourceKey = strings.TrimSpace(sourceKey)
	if sourceKey == "" {
		return nil
	}

	var mappings []model.CategoryMapping
	if err := db.Mdb.Find(&mappings).Error; err != nil {
		log.Printf("currentProvideCategoryIDsBySourceKey Error: %v", err)
		return nil
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
	return ids
}

func resolveProvideCurrentCategoryIDFromSnapshot(snapshot model.FilmListSnapshot) int64 {
	if ids := currentProvideCategoryIDsBySourceKey(snapshot.CategoryKey); len(ids) > 0 {
		return ids[0]
	}
	if ids := currentProvideCategoryIDsBySourceKey(snapshot.RootCategoryKey); len(ids) > 0 {
		return ids[0]
	}
	if snapshot.Cid > 0 {
		return repository.ResolveCategoryID(snapshot.Cid)
	}
	if snapshot.Pid > 0 {
		return repository.ResolveCategoryID(snapshot.Pid)
	}
	return 0
}

func resolveProvideCurrentRootCategoryIDFromSnapshot(snapshot model.FilmListSnapshot) int64 {
	if ids := currentProvideCategoryIDsBySourceKey(snapshot.RootCategoryKey); len(ids) > 0 {
		rootID := repository.GetRootId(ids[0])
		if rootID > 0 {
			return rootID
		}
		return ids[0]
	}
	if categoryID := resolveProvideCurrentCategoryIDFromSnapshot(snapshot); categoryID > 0 {
		rootID := repository.GetRootId(categoryID)
		if rootID > 0 {
			return rootID
		}
		return categoryID
	}
	return 0
}

func resolveProvideTypeFromSnapshot(snapshot model.FilmListSnapshot) (int64, string) {
	if categoryID := resolveProvideCurrentCategoryIDFromSnapshot(snapshot); categoryID > 0 {
		if name := repository.GetCategoryNameById(categoryID); name != "" {
			return categoryID, name
		}
		if name := repository.GetMainCategoryName(categoryID); name != "" {
			return categoryID, name
		}
	}
	if snapshot.Cid > 0 {
		if name := repository.GetCategoryNameById(snapshot.Cid); name != "" {
			return snapshot.Cid, name
		}
		return snapshot.Cid, snapshot.CName
	}
	if snapshot.Pid > 0 {
		if name := repository.GetMainCategoryName(snapshot.Pid); name != "" {
			return snapshot.Pid, name
		}
		return snapshot.Pid, snapshot.CName
	}
	return 0, snapshot.CName
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
