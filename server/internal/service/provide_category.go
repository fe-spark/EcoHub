package service

import (
	"strconv"
	"strings"

	"server/internal/model"
	"server/internal/repository"
)

func parseProvideSourceCategoryKey(key string) (string, int64, bool) {
	key = strings.TrimSpace(key)
	const prefix = "source:"
	if !strings.HasPrefix(key, prefix) {
		return "", 0, false
	}
	rest := key[len(prefix):]
	idx := strings.LastIndex(rest, ":")
	if idx <= 0 || idx >= len(rest)-1 {
		return "", 0, false
	}
	typeID, err := strconv.ParseInt(rest[idx+1:], 10, 64)
	if err != nil || typeID <= 0 {
		return "", 0, false
	}
	sourceID := strings.TrimSpace(rest[:idx])
	if sourceID == "" {
		return "", 0, false
	}
	return sourceID, typeID, true
}

func provideTypeFromKey(sourceID, key string) (int64, string, bool) {
	parsedSource, typeID, ok := parseProvideSourceCategoryKey(key)
	if !ok || parsedSource != strings.TrimSpace(sourceID) {
		return 0, "", false
	}
	return typeID, repository.PublicSourceTypeName(sourceID, typeID), true
}

func resolveProvideListedType(snapshot model.FilmListSnapshot, sourceID string) (int64, string) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID != "" {
		if typeID, name, ok := provideTypeFromKey(sourceID, snapshot.CategoryKey); ok {
			if name == "" {
				name = snapshot.CName
			}
			return typeID, name
		}
		if typeID, name, ok := provideTypeFromKey(sourceID, snapshot.RootCategoryKey); ok {
			if name == "" {
				name = snapshot.CName
			}
			return typeID, name
		}
	}
	return resolveProvideTypeFromSnapshot(snapshot)
}

func resolveProvideListedRootType(snapshot model.FilmListSnapshot, sourceID string) int64 {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID != "" {
		if typeID, _, ok := provideTypeFromKey(sourceID, snapshot.RootCategoryKey); ok {
			return typeID
		}
	}
	return resolveProvideCurrentRootCategoryIDFromSnapshot(snapshot)
}

func providePlayFromName(link model.PlayLinkVo) string {
	name := strings.TrimSpace(link.Name)
	if !link.Proxy {
		return name
	}
	if name == "" {
		return "代理"
	}
	if strings.HasSuffix(name, "代理") {
		return name
	}
	return name + "代理"
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
