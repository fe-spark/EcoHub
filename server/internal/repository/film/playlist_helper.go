package film

import (
	"encoding/json"
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

func ExtractFirstPlayableList(contentByKey map[string]string, orderedKeys []string) []model.MovieUrlInfo {
	for _, k := range orderedKeys {
		content, ok := contentByKey[k]
		if !ok || content == "" {
			continue
		}
		var allPlayList [][]model.MovieUrlInfo
		if err := json.Unmarshal([]byte(content), &allPlayList); err == nil && len(allPlayList) > 0 && len(allPlayList[0]) > 0 {
			return allPlayList[0]
		}
	}
	return nil
}
