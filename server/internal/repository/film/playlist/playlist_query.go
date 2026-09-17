package playlist

import (
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/film/shared"

	"gorm.io/gorm"
)

func GetMultiplePlayGroupsBySourcesAndKeys(sources []model.FilmSource, keys []string) map[string][]model.PlayLinkVo {
	orderedKeys := shared.DropSharedMatchKeys(shared.UniqueKeys(keys))
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

	playlistsBySourceKey, err := shared.LoadPlaylistsBySourceAndKeysTx(db.Mdb, sourceIDs, orderedKeys)
	if err != nil {
		return nil
	}
	if len(playlistsBySourceKey) == 0 {
		return nil
	}

	result := make(map[string][]model.PlayLinkVo, len(sources))
	for _, source := range sources {
		groups := shared.BuildPlayGroupsFromLoadedPlaylists(source.Id, source.Name, orderedKeys, playlistsBySourceKey)
		if len(groups) > 0 {
			result[source.Id] = groups
		}
	}
	return result
}

func getMultiplePlayGroupsByKeysTx(tx *gorm.DB, siteID, siteName string, keys []string) []model.PlayLinkVo {
	orderedKeys := shared.DropSharedMatchKeys(shared.UniqueKeys(keys))
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

	return shared.SelectBestPlayGroups(siteID, siteName, orderedKeys, playlistByKey)
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
