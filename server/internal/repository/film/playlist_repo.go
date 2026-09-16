package film

import (
	"encoding/json"
	"log"
	"strings"

	"server/internal/model"
	"server/internal/repository"
)

// SaveSitePlayList 写入附属站播放列表。
// NotifyMIDs：本源追集且集数超过全库其它源（进最近更新）。
// AffectedMIDs：任意 playlist 实质写入（含追平主站、仅链接刷新），详情缓存必须失效并展示最新集。
func SaveSitePlayList(sourceID string, list []model.MovieDetail) (CollectWriteResult, error) {
	if len(list) == 0 {
		return CollectWriteResult{}, nil
	}

	var playlists []model.SlaveMoviePlaylist
	keysByMovieKey := make(map[string]struct{}, len(list)*2)

	detailMids, primaryKeyByMid, matchedInfos, keysByMid, err := matchSlaveDetailMids(list)
	if err != nil {
		return CollectWriteResult{}, err
	}
	inheritedKeyByLookup := loadInheritedKeysForUnmatchedDetails(list, detailMids)

	for index, detail := range list {
		if !isPlaylistWritableDetail(detail) {
			continue
		}

		writeKeys := playlistWriteKeys(detail, detailMids[index], primaryKeyByMid, inheritedKeyByLookup)
		for _, movieKey := range writeKeys {
			keysByMovieKey[movieKey] = struct{}{}

			for groupIndex, links := range detail.PlayList {
				if len(links) == 0 {
					continue
				}

				data, _ := json.Marshal(links)
				rawName := ""
				if groupIndex < len(detail.PlayFrom) {
					rawName = strings.TrimSpace(detail.PlayFrom[groupIndex])
				}

				playlists = append(playlists, model.SlaveMoviePlaylist{
					SourceId:   sourceID,
					MovieKey:   movieKey,
					GroupIndex: groupIndex,
					GroupName:  rawName,
					Content:    string(data),
				})
			}
		}
	}

	if len(keysByMovieKey) == 0 {
		return CollectWriteResult{}, nil
	}

	changes, err := saveGroupedPlaylists(sourceID, playlists, keysByMovieKey)
	if err != nil {
		log.Printf("SaveSitePlayList Error: %v", err)
		return CollectWriteResult{}, err
	}
	// 仅在有播放源实质变更时更新 last_collect_time。
	if len(changes) > 0 {
		repository.NoteCollectSourceStats(sourceID)
	}

	// 无变更短路：若本批次没有任何播放列表实质变更，且非海报同步源，
	// 说明所有数据已处于最新同步状态，跳过后续所有映射与刷新，耗时降至 0。
	if len(changes) == 0 && !isSourcePosterSyncConfigured(sourceID) {
		return CollectWriteResult{}, nil
	}

	result, err := scheduleSearchInfoRefreshByPlaylists(sourceID, list, changes, matchedInfos, keysByMid)
	if err != nil {
		log.Printf("scheduleSearchInfoRefreshByPlaylists Error: %v", err)
		return CollectWriteResult{}, err
	}

	return result, nil
}

func isSourcePosterSyncConfigured(sourceID string) bool {
	src := repository.FindCollectSourceById(sourceID)
	return src != nil && src.IsPosterSource && src.State
}

func isPlaylistWritableDetail(detail model.MovieDetail) bool {
	return len(detail.PlayList) > 0 && !strings.Contains(detail.CName, "解说")
}

// playlistWriteKeys 一条附属站详情落到哪些 movie_key。唯一命中写该片主键；否则写候选键（有大类则不含纯片名）。
func playlistWriteKeys(
	detail model.MovieDetail,
	mid int64,
	primaryKeyByMid map[int64]string,
	inheritedKeyByLookup map[string]string,
) []string {
	if mid > 0 && primaryKeyByMid[mid] != "" {
		return []string{primaryKeyByMid[mid]}
	}
	if ResolveMovieDetailRootPid(detail) == 0 {
		for _, lookupKey := range BuildPlaylistMovieKeys(detail) {
			if inherited := inheritedKeyByLookup[lookupKey]; inherited != "" {
				return []string{inherited}
			}
		}
	}
	return BuildPlaylistCandidateKeys(detail)
}
