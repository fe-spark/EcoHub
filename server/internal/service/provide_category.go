package service

import (
	"strings"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/support"
)

func parseProvideSourceCategoryKey(key string) (string, int64, bool) {
	return support.ParseSourceCategoryKey(key)
}

func provideTypeFromKey(sourceID, key string) (int64, string, bool) {
	parsedSource, typeID, ok := parseProvideSourceCategoryKey(key)
	if !ok || parsedSource != strings.TrimSpace(sourceID) {
		return 0, "", false
	}
	return typeID, repository.PublicSourceTypeName(sourceID, typeID), true
}

func resolveProvideListedType(snapshot model.FilmListSnapshot, sourceID string) (int64, string, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID != "" {
		if typeID, name, ok := provideTypeFromKey(sourceID, snapshot.CategoryKey); ok {
			if name == "" {
				name = snapshot.CName
			}
			return typeID, name, nil
		}
		if typeID, name, ok := provideTypeFromKey(sourceID, snapshot.RootCategoryKey); ok {
			if name == "" {
				name = snapshot.CName
			}
			return typeID, name, nil
		}
	}
	return resolveProvideTypeFromSnapshot(snapshot)
}

func resolveProvideListedRootType(snapshot model.FilmListSnapshot, sourceID string) (int64, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID != "" {
		if typeID, _, ok := provideTypeFromKey(sourceID, snapshot.RootCategoryKey); ok {
			return typeID, nil
		}
	}
	return resolveProvideCurrentRootCategoryIDFromSnapshot(snapshot)
}

// resolveProvideSourceID 未指定订阅源时用当前主站。TVBox 没有全量入口。
func resolveProvideSourceID(sourceID string) string {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID != "" {
		return sourceID
	}
	if active := repository.GetActiveCollectSource(); active != nil {
		return strings.TrimSpace(active.Id)
	}
	return ""
}
