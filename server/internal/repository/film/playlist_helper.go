package film

import (
	"fmt"
	"strings"

	"server/internal/model"
	"server/internal/utils"
)

func BuildPlaylistMovieKeys(detail model.MovieDetail) []string {
	keys := make([]string, 0, 2)
	if dbIdentity := utils.BuildCollectionDbIdentity(detail.DbId, detail.Name); dbIdentity != "" {
		keys = append(keys, utils.GenerateHashKey(dbIdentity))
	}
	normalizedTitle := utils.NormalizeCollectionTitle(detail.Name)
	if normalizedTitle != "" {
		keys = append(keys, utils.GenerateHashKey(normalizedTitle))
	}
	return keys
}

func BuildMovieLookupKeys(mid int64, name string) []string {
	keys := make([]string, 0, 2)
	if dbIdentity := utils.BuildCollectionDbIdentity(mid, name); dbIdentity != "" {
		keys = append(keys, utils.GenerateHashKey(dbIdentity))
	}
	if normalizedTitle := utils.NormalizeCollectionTitle(name); normalizedTitle != "" {
		keys = append(keys, utils.GenerateHashKey(normalizedTitle))
	}
	return UniqueKeys(keys)
}

func BuildValidPlaylistKeys(films []struct {
	Name string
	DbId int64
}) map[string]struct{} {
	validKeys := make(map[string]struct{}, len(films)*4)
	for _, f := range films {
		if normalizedTitle := utils.NormalizeCollectionTitle(f.Name); normalizedTitle != "" {
			validKeys[utils.GenerateHashKey(normalizedTitle)] = struct{}{}
		}
		if dbIdentity := utils.BuildCollectionDbIdentity(f.DbId, f.Name); dbIdentity != "" {
			validKeys[utils.GenerateHashKey(dbIdentity)] = struct{}{}
		}
	}
	return validKeys
}

func UniqueKeys(keys []string) []string {
	orderedKeys := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		orderedKeys = append(orderedKeys, k)
	}
	return orderedKeys
}

// BuildDisplaySourceName 统一站点播放源展示名。
// 单线路仅展示站点名，多线路优先使用“站点名-原始线路名”，缺失时降级为“站点名-线路N”。
func BuildDisplaySourceName(siteName, rawName string, index, total int) string {
	siteName = strings.TrimSpace(siteName)
	rawName = strings.TrimSpace(rawName)

	if siteName == "" {
		if rawName != "" {
			return rawName
		}
		if total > 1 {
			return fmt.Sprintf("播放源%d", index+1)
		}
		return "默认源"
	}

	if total <= 1 {
		return siteName
	}
	if rawName != "" {
		return fmt.Sprintf("%s-%s", siteName, rawName)
	}
	return fmt.Sprintf("%s-线路%d", siteName, index+1)
}
