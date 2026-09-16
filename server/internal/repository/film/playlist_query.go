package film

import (
	"encoding/json"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

func GetMultiplePlayGroupsByKeys(siteID, siteName string, keys []string) []model.PlayLinkVo {
	return getMultiplePlayGroupsByKeysTx(db.Mdb, siteID, siteName, keys)
}

func GetMultiplePlayGroupsBySourcesAndKeys(sources []model.FilmSource, keys []string) map[string][]model.PlayLinkVo {
	orderedKeys := UniqueKeys(keys)
	if len(sources) == 0 || len(orderedKeys) == 0 {
		return nil
	}

	sourceIDs := make([]string, 0, len(sources))
	for _, source := range sources {
		if strings.TrimSpace(source.Id) != "" {
			sourceIDs = append(sourceIDs, source.Id)
		}
	}
	if len(sourceIDs) == 0 {
		return nil
	}

	playlistsBySourceKey, err := loadPlaylistsBySourceAndKeysTx(db.Mdb, sourceIDs, orderedKeys)
	if err != nil {
		return nil
	}
	if len(playlistsBySourceKey) == 0 {
		return nil
	}

	result := make(map[string][]model.PlayLinkVo, len(sources))
	for _, source := range sources {
		groups := buildPlayGroupsFromLoadedPlaylists(source.Id, source.Name, orderedKeys, playlistsBySourceKey)
		if len(groups) > 0 {
			result[source.Id] = groups
		}
	}
	return result
}

func getMultiplePlayGroupsByKeysTx(tx *gorm.DB, siteID, siteName string, keys []string) []model.PlayLinkVo {
	orderedKeys := UniqueKeys(keys)
	if siteID == "" || len(orderedKeys) == 0 {
		return nil
	}

	var playlists []model.SlaveMoviePlaylist
	if err := tx.Where("source_id = ? AND movie_key IN ?", siteID, orderedKeys).
		Order("movie_key ASC").
		Order("group_index ASC").
		Find(&playlists).Error; err != nil {
		return nil
	}
	if len(playlists) == 0 {
		return nil
	}

	playlistByKey := make(map[string][]model.SlaveMoviePlaylist, len(playlists))
	for _, playlist := range playlists {
		playlistByKey[playlist.MovieKey] = append(playlistByKey[playlist.MovieKey], playlist)
	}

	return selectBestPlayGroups(siteID, siteName, orderedKeys, playlistByKey)
}

func loadPlaylistGroupsByInfos(infos []model.FilmIndex) (map[int64]map[string][]model.PlayLinkVo, error) {
	return loadPlaylistGroupsByInfosTx(db.Mdb, infos)
}

func loadPlaylistGroupsByInfosTx(tx *gorm.DB, infos []model.FilmIndex) (map[int64]map[string][]model.PlayLinkVo, error) {
	result := make(map[int64]map[string][]model.PlayLinkVo, len(infos))
	mids := make([]int64, 0, len(infos))
	for _, info := range infos {
		if info.Mid > 0 {
			mids = append(mids, info.Mid)
		}
	}

	keysByMid := loadMovieMatchKeysByMidsTx(tx, mids)
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

	playlistsBySourceKey, err := loadPlaylistsBySourceAndKeysTx(tx, sourceIDs, allKeys)
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
			groups := buildPlayGroupsFromLoadedPlaylists(source.Id, source.Name, lookupKeys, playlistsBySourceKey)
			if len(groups) == 0 {
				continue
			}
			groupsBySource[source.Id] = groups
		}
		result[info.Mid] = groupsBySource
	}
	return result, nil
}

func loadPlaylistsBySourceAndKeysTx(tx *gorm.DB, sourceIDs []string, keys []string) (map[string]map[string][]model.SlaveMoviePlaylist, error) {
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

func buildPlayGroupsFromLoadedPlaylists(
	siteID string,
	siteName string,
	keys []string,
	playlistsBySourceKey map[string]map[string][]model.SlaveMoviePlaylist,
) []model.PlayLinkVo {
	byKey := playlistsBySourceKey[siteID]
	if len(byKey) == 0 {
		return nil
	}
	return selectBestPlayGroups(siteID, siteName, keys, byKey)
}

func selectBestPlayGroups(siteID, siteName string, keys []string, byKey map[string][]model.SlaveMoviePlaylist) []model.PlayLinkVo {
	var best []model.PlayLinkVo
	bestCount := -1
	for _, key := range UniqueKeys(keys) {
		groups := playGroupsFromPlaylistRows(siteID, siteName, byKey[key])
		if len(groups) == 0 {
			continue
		}
		count := 0
		for _, group := range groups {
			if n := episodeCount(group.LinkList); n > count {
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

// LoadSourceMidByGlobalMid 通过全局影片 ID 获取指定站点的原始影片 ID。
// 单片更新全部站点时，主站和附属站都会先经过这里做一次 ID 翻译。
func LoadSourceMidByGlobalMid(globalMid int64, sourceID string) int64 {
	if globalMid <= 0 || strings.TrimSpace(sourceID) == "" {
		return 0
	}

	var mapping model.MovieSourceMapping
	if err := db.Mdb.Where("global_mid = ? AND source_id = ?", globalMid, sourceID).First(&mapping).Error; err != nil {
		return 0
	}
	return mapping.SourceMid
}
