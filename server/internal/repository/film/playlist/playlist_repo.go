package playlist

import (
	"encoding/json"
	"log"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/film/shared"

	"gorm.io/gorm"
)

// SaveSitePlayList 写入附属站播放列表。
// NotifyMIDs：本源追集且集数超过全库其它源（进最近更新）。
// AffectedMIDs：任意 playlist 实质写入（含追平主站、仅链接刷新），详情缓存必须失效并展示最新集。
func SaveSitePlayList(sourceID string, list []model.MovieDetail) (shared.CollectWriteResult, error) {
	if len(list) == 0 {
		return shared.CollectWriteResult{}, nil
	}

	var playlists []model.SlaveMoviePlaylist
	keysByMovieKey := make(map[string]struct{}, len(list)*2)

	detailMids, primaryKeyByMid, matchedInfos, keysByMid, infoByMid, err := matchSlaveDetailMids(list)
	if err != nil {
		return shared.CollectWriteResult{}, err
	}
	inheritedKeyByLookup := loadInheritedKeysForUnmatchedDetails(list, detailMids)
	exclusiveOwnerByKey := exclusiveMatchKeyOwners(keysByMid)

	for index, detail := range list {
		if !isPlaylistWritableDetail(detail) {
			continue
		}

		if mid := detailMids[index]; mid > 0 {
			incoming := shared.IdentityFromMovieDetail(detail)
			for _, siblingKey := range exclusiveKeysOfTitleSiblings(mid, keysByMid) {
				if siblingPlaylistLooksLikeIncoming(sourceID, siblingKey, incoming) {
					keysByMovieKey[siblingKey] = struct{}{}
				}
			}
		}

		writeKeys := playlistWriteKeys(detail, detailMids[index], primaryKeyByMid, inheritedKeyByLookup, exclusiveOwnerByKey, infoByMid)
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
		return shared.CollectWriteResult{}, nil
	}

	changes, err := saveGroupedPlaylists(sourceID, playlists, keysByMovieKey)
	if err != nil {
		log.Printf("SaveSitePlayList Error: %v", err)
		return shared.CollectWriteResult{}, err
	}
	// 仅在有播放源实质变更时更新 last_collect_time。
	if len(changes) > 0 {
		repository.NoteCollectSourceStats(sourceID)
	}

	// 无变更短路：若本批次没有任何播放列表实质变更，且非海报同步源，
	// 说明所有数据已处于最新同步状态，跳过后续所有映射与刷新，耗时降至 0。
	if len(changes) == 0 && !isSourcePosterSyncConfigured(sourceID) {
		return shared.CollectWriteResult{}, nil
	}

	result, err := scheduleSearchInfoRefreshByPlaylists(sourceID, list, changes, matchedInfos, keysByMid)
	if err != nil {
		log.Printf("scheduleSearchInfoRefreshByPlaylists Error: %v", err)
		return shared.CollectWriteResult{}, err
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
// 未绑定时空过已被另一部独占的键，避免同名跨类把线路写进别人的主键槽。
func playlistWriteKeys(
	detail model.MovieDetail,
	mid int64,
	primaryKeyByMid map[int64]string,
	inheritedKeyByLookup map[string]string,
	exclusiveOwnerByKey map[string]int64,
	infoByMid map[int64]model.FilmIndex,
) []string {
	if mid > 0 && primaryKeyByMid[mid] != "" {
		return []string{primaryKeyByMid[mid]}
	}
	if shared.ResolveMovieDetailRootPid(detail) == 0 {
		incoming := shared.IdentityFromMovieDetail(detail)
		for _, lookupKey := range shared.BuildPlaylistMovieKeys(detail) {
			inherited := inheritedKeyByLookup[lookupKey]
			if inherited == "" {
				continue
			}
			if !inheritedKeyCompatible(inherited, incoming, exclusiveOwnerByKey, infoByMid) {
				continue
			}
			return []string{inherited}
		}
	}
	return dropKeysOwnedBySingleFilm(BuildPlaylistCandidateKeys(detail), exclusiveOwnerByKey)
}

func inheritedKeyCompatible(
	inherited string,
	incoming shared.IdentityProfile,
	exclusiveOwnerByKey map[string]int64,
	infoByMid map[int64]model.FilmIndex,
) bool {
	owner := exclusiveOwnerByKey[inherited]
	if owner <= 0 {
		return true
	}
	info, ok := infoByMid[owner]
	if !ok {
		return true
	}
	master := shared.IdentityFromFilmIndex(info)
	return shared.PickUniqueIdentityMid(incoming, map[int64]shared.IdentityProfile{owner: master}) == owner
}

func exclusiveMatchKeyOwners(keysByMid map[int64][]string) map[string]int64 {
	owners := make(map[string]int64, len(keysByMid)*2)
	for mid, keys := range keysByMid {
		if mid <= 0 {
			continue
		}
		for _, key := range keys {
			if key == "" {
				continue
			}
			if prev, ok := owners[key]; ok && prev != mid {
				owners[key] = 0
				continue
			}
			owners[key] = mid
		}
	}
	return owners
}

func dropKeysOwnedBySingleFilm(keys []string, exclusiveOwnerByKey map[string]int64) []string {
	if len(keys) == 0 || len(exclusiveOwnerByKey) == 0 {
		return keys
	}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if exclusiveOwnerByKey[key] > 0 {
			continue
		}
		out = append(out, key)
	}
	return out
}

func exclusiveKeysOfTitleSiblings(matchedMid int64, keysByMid map[int64][]string) []string {
	matchedKeys := make(map[string]struct{}, len(keysByMid[matchedMid]))
	for _, key := range keysByMid[matchedMid] {
		if key != "" {
			matchedKeys[key] = struct{}{}
		}
	}
	if len(matchedKeys) == 0 {
		return nil
	}
	drop := make([]string, 0)
	seen := make(map[string]struct{})
	for mid, keys := range keysByMid {
		if mid == matchedMid {
			continue
		}
		sharesTitle := false
		for _, key := range keys {
			if _, ok := matchedKeys[key]; ok {
				sharesTitle = true
				break
			}
		}
		if !sharesTitle {
			continue
		}
		for _, key := range keys {
			if key == "" {
				continue
			}
			if _, shared := matchedKeys[key]; shared {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			drop = append(drop, key)
		}
	}
	return drop
}

func siblingPlaylistLooksLikeIncoming(sourceID, movieKey string, incoming shared.IdentityProfile) bool {
	if db.Mdb == nil || strings.TrimSpace(movieKey) == "" {
		return false
	}
	var rows []model.SlaveMoviePlaylist
	if err := db.Mdb.Where("source_id = ? AND movie_key = ?", sourceID, movieKey).Find(&rows).Error; err != nil || len(rows) == 0 {
		return false
	}
	for _, row := range rows {
		var links []model.MovieUrlInfo
		if err := json.Unmarshal([]byte(row.Content), &links); err != nil {
			continue
		}
		if shared.SameWorkPlaylist(links, incoming) {
			return true
		}
	}
	return false
}

// ReviveSlavePlaylistsTx 保持接口向后兼容（单阶段极简治理模式下已无须维护观察期状态机打标）。
func ReviveSlavePlaylistsTx(tx *gorm.DB, matchKeys []string) error {
	return nil
}
