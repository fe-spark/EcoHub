package shared

import (
	"encoding/json"
	"fmt"
	"strings"

	"server/internal/model"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

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

func LoadPlaylistGroupsByInfosTx(tx *gorm.DB, infos []model.FilmIndex) (map[int64]map[string][]model.PlayLinkVo, error) {
	result := make(map[int64]map[string][]model.PlayLinkVo, len(infos))
	mids := make([]int64, 0, len(infos))
	for _, info := range infos {
		if info.Mid > 0 {
			mids = append(mids, info.Mid)
		}
	}

	keysByMid := LoadMovieMatchKeysByMidsTx(tx, mids)
	allKeys := make([]string, 0, len(infos)*4)
	for _, keys := range keysByMid {
		allKeys = append(allKeys, keys...)
	}
	allKeys = UniqueKeys(allKeys)

	sources := make([]model.FilmSource, 0)
	sourceIDs := make([]string, 0)
	for _, source := range support.GetCollectSourceList() {
		if source.Grade != model.SlaveCollect || !source.State {
			continue
		}
		sources = append(sources, source)
		sourceIDs = append(sourceIDs, source.Id)
	}

	playlistsBySourceKey, err := LoadPlaylistsBySourceAndKeysTx(tx, sourceIDs, allKeys)
	if err != nil {
		return nil, err
	}
	for _, info := range infos {
		groupsBySource := make(map[string][]model.PlayLinkVo)
		lookupKeys := keysByMid[info.Mid]
		if len(lookupKeys) == 0 || len(playlistsBySourceKey) == 0 {
			result[info.Mid] = groupsBySource
			continue
		}

		for _, source := range sources {
			groups := BuildPlayGroupsFromLoadedPlaylists(source.Id, source.Name, lookupKeys, playlistsBySourceKey)
			if len(groups) == 0 {
				continue
			}
			groupsBySource[source.Id] = groups
		}
		result[info.Mid] = groupsBySource
	}
	return result, nil
}

func LoadPlaylistsBySourceAndKeysTx(tx *gorm.DB, sourceIDs []string, keys []string) (map[string]map[string][]model.SlaveMoviePlaylist, error) {
	if len(sourceIDs) == 0 || len(keys) == 0 {
		return nil, nil
	}

	var playlists []model.SlaveMoviePlaylist
	if err := tx.Where("source_id IN ? AND movie_key IN ?", sourceIDs, keys).
		Order("source_id ASC").
		Order("movie_key ASC").
		Order("group_index ASC").
		Find(&playlists).Error; err != nil {
		return nil, err
	}

	result := make(map[string]map[string][]model.SlaveMoviePlaylist)
	for _, playlist := range playlists {
		byKey := result[playlist.SourceId]
		if byKey == nil {
			byKey = make(map[string][]model.SlaveMoviePlaylist)
			result[playlist.SourceId] = byKey
		}
		byKey[playlist.MovieKey] = append(byKey[playlist.MovieKey], playlist)
	}
	return result, nil
}

func BuildPlayGroupsFromLoadedPlaylists(
	siteID string,
	siteName string,
	keys []string,
	playlistsBySourceKey map[string]map[string][]model.SlaveMoviePlaylist,
) []model.PlayLinkVo {
	byKey := playlistsBySourceKey[siteID]
	if len(byKey) == 0 {
		return nil
	}
	return SelectBestPlayGroups(siteID, siteName, keys, byKey)
}

func SelectBestPlayGroups(siteID, siteName string, keys []string, byKey map[string][]model.SlaveMoviePlaylist) []model.PlayLinkVo {
	var best []model.PlayLinkVo
	bestCount := -1
	for _, key := range UniqueKeys(keys) {
		groups := playGroupsFromPlaylistRows(siteID, siteName, byKey[key])
		if len(groups) == 0 {
			continue
		}
		count := 0
		for _, group := range groups {
			if n := EpisodeCount(group.LinkList); n > count {
				count = n
			}
		}
		if count > bestCount {
			best = groups
			bestCount = count
		}
	}
	return best
}

func playGroupsFromPlaylistRows(siteID, siteName string, matched []model.SlaveMoviePlaylist) []model.PlayLinkVo {
	if len(matched) == 0 {
		return nil
	}
	groups := make([]model.PlayLinkVo, 0, len(matched))
	for _, playlist := range matched {
		var links []model.MovieUrlInfo
		if err := json.Unmarshal([]byte(playlist.Content), &links); err != nil || len(links) == 0 {
			continue
		}
		displayName := BuildDisplaySourceName(siteName, playlist.GroupName, playlist.GroupIndex, len(matched))
		groupID := BuildPlayGroupID(siteID, playlist.GroupName, playlist.GroupIndex, len(matched))
		groups = append(groups, model.PlayLinkVo{
			Id:       groupID,
			SourceId: siteID,
			Name:     displayName,
			LinkList: links,
		})
	}
	return groups
}
