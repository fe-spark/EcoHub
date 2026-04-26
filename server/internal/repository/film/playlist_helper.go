package film

import (
	"fmt"
	"strings"

	"server/internal/model"
	"server/internal/utils"
)

func BuildPlaylistMovieKeys(detail model.MovieDetail) []string {
	return BuildMovieMatchKeys(detail.DbId, detail.Name)
}

func BuildMovieMatchKeys(dbID int64, name string) []string {
	keys := make([]string, 0, 2)
	if dbIdentity := utils.BuildCollectionDbIdentity(dbID, name); dbIdentity != "" {
		keys = append(keys, utils.GenerateHashKey(dbIdentity))
	}

	normalizedTitle := utils.NormalizeCollectionTitle(name)
	if normalizedTitle != "" {
		keys = append(keys, utils.GenerateHashKey(normalizedTitle))
	}
	return UniqueKeys(keys)
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

// BuildPlayGroupID 为单个可播放分组构建稳定 ID。
// 单线路时直接复用站点 ID，多线路时追加线路标识，确保同站不同线路可区分。
func BuildPlayGroupID(sourceID, rawName string, index, total int) string {
	sourceID = strings.TrimSpace(sourceID)
	rawName = strings.TrimSpace(rawName)
	if sourceID == "" {
		if rawName != "" {
			return rawName
		}
		return fmt.Sprintf("group_%d", index)
	}
	if total <= 1 {
		return sourceID
	}
	if rawName != "" {
		return fmt.Sprintf("%s::%s", sourceID, rawName)
	}
	return fmt.Sprintf("%s::group_%d", sourceID, index)
}
