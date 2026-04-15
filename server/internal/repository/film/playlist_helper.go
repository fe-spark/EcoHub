package film

import (
	"encoding/json"
	"strings"

	"server/internal/model"
	"server/internal/utils"
)

func BuildPlaylistMovieKeys(detail model.MovieDetail) []string {
	keys := make([]string, 0, 2)
	if detail.DbId != 0 {
		keys = append(keys, utils.GenerateHashKey(detail.DbId))
	}
	keys = append(keys, utils.GenerateHashKey(detail.Name))
	return keys
}

func BuildMovieLookupKeys(mid int64, name string) []string {
	keys := make([]string, 0, 2)
	if mid != 0 {
		keys = append(keys, utils.GenerateHashKey(mid))
	}
	if strings.TrimSpace(name) != "" {
		keys = append(keys, utils.GenerateHashKey(name))
	}
	return UniqueKeys(keys)
}

func BuildValidPlaylistKeys(films []struct {
	Name string
	DbId int64
}) map[string]struct{} {
	validKeys := make(map[string]struct{}, len(films)*4)
	for _, f := range films {
		for _, c := range utils.NormalizeTitleCandidates(f.Name) {
			validKeys[utils.GenerateHashKey(c)] = struct{}{}
		}
		if f.DbId != 0 {
			validKeys[utils.GenerateHashKey(f.DbId)] = struct{}{}
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
