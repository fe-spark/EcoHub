package film

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func SaveSitePlayList(id string, list []model.MovieDetail) error {
	if len(list) <= 0 {
		return nil
	}

	var playlists []model.MoviePlaylist
	keysByMovieKey := make(map[string]struct{}, len(list)*2)
forLoop:
	for _, d := range list {
		if len(d.PlayList) == 0 || strings.Contains(d.CName, "解说") {
			continue forLoop
		}

		for _, movieKey := range BuildPlaylistMovieKeys(d) {
			keysByMovieKey[movieKey] = struct{}{}
			for index, links := range d.PlayList {
				if len(links) == 0 {
					continue
				}

				data, _ := json.Marshal(links)
				rawName := ""
				if index < len(d.PlayFrom) {
					rawName = strings.TrimSpace(d.PlayFrom[index])
				}

				playlists = append(playlists, model.MoviePlaylist{
					SourceId:   id,
					MovieKey:   movieKey,
					GroupIndex: index,
					GroupName:  rawName,
					Content:    string(data),
				})
			}
		}
	}

	if len(playlists) == 0 {
		return nil
	}

	if err := saveGroupedPlaylists(id, playlists, keysByMovieKey); err != nil {
		log.Printf("SaveSitePlayList Error: %v", err)
		return err
	}

	if err := refreshSearchInfosByPlaylists(list); err != nil {
		log.Printf("refreshSearchInfosByPlaylists Error: %v", err)
		return err
	}

	log.Printf("[Playlist] 为站点 %s 保存了 %d 条记录\n", id, len(playlists))
	return nil
}

func refreshSearchInfosByPlaylists(details []model.MovieDetail) error {
	infos, latestByMid, err := loadMatchedSearchInfosByDetails(details)
	if err != nil {
		return err
	}
	if err := refreshLatestSourceStampByMids(latestByMid); err != nil {
		return err
	}
	return RefreshPlayFromSummaryBySearchInfos(infos)
}

func loadMatchedSearchInfosByDetails(details []model.MovieDetail) ([]model.SearchInfo, map[int64]int64, error) {
	type detailLookup struct {
		detail model.MovieDetail
		keys   []string
	}
	lookups := make([]detailLookup, 0, len(details))
	allKeys := make([]string, 0, len(details)*4)
	latestByMid := make(map[int64]int64)

	for _, detail := range details {
		lookupKeys := BuildPlaylistMovieKeys(detail)
		if len(lookupKeys) == 0 {
			continue
		}
		lookups = append(lookups, detailLookup{detail: detail, keys: lookupKeys})
		allKeys = append(allKeys, lookupKeys...)
	}

	if len(lookups) == 0 {
		return nil, latestByMid, nil
	}

	midsByLookupKey := loadMidCandidatesByMatchKeys(allKeys)
	matchedMidSet := make(map[int64]struct{}, len(allKeys))
	for _, mids := range midsByLookupKey {
		for _, mid := range mids {
			matchedMidSet[mid] = struct{}{}
		}
	}
	if len(matchedMidSet) == 0 {
		return nil, latestByMid, nil
	}

	matchedMids := make([]int64, 0, len(matchedMidSet))
	for mid := range matchedMidSet {
		matchedMids = append(matchedMids, mid)
	}

	var candidates []model.SearchInfo
	if err := db.Mdb.Where("mid IN ?", matchedMids).Find(&candidates).Error; err != nil {
		return nil, nil, err
	}
	if len(candidates) == 0 {
		return nil, latestByMid, nil
	}

	infoByMid := make(map[int64]model.SearchInfo, len(candidates))
	for _, info := range candidates {
		infoByMid[info.Mid] = info
	}

	ordered := make([]model.SearchInfo, 0, len(candidates))
	seenMid := make(map[int64]struct{}, len(candidates))
	for _, item := range lookups {
		stamp, err := time.ParseInLocation(time.DateTime, item.detail.UpdateTime, time.Local)
		if err != nil {
			stamp = time.Time{}
		}
		matched := make(map[int64]struct{}, 2)
		for _, key := range item.keys {
			candidateMids := midsByLookupKey[key]
			if len(candidateMids) == 0 {
				continue
			}
			for _, mid := range candidateMids {
				matched[mid] = struct{}{}
			}
			break
		}
		for mid := range matched {
			if stampUnix := stamp.Unix(); stampUnix > latestByMid[mid] {
				latestByMid[mid] = stampUnix
			}
			if _, ok := seenMid[mid]; ok {
				continue
			}
			seenMid[mid] = struct{}{}
			ordered = append(ordered, infoByMid[mid])
		}
	}

	return ordered, latestByMid, nil
}

func saveGroupedPlaylists(sourceID string, playlists []model.MoviePlaylist, keysByMovieKey map[string]struct{}) error {
	movieKeys := make([]string, 0, len(keysByMovieKey))
	for movieKey := range keysByMovieKey {
		if strings.TrimSpace(movieKey) == "" {
			continue
		}
		movieKeys = append(movieKeys, movieKey)
	}

	return db.Mdb.Transaction(func(tx *gorm.DB) error {
		if len(movieKeys) > 0 {
			if err := tx.Unscoped().Where("source_id = ? AND movie_key IN ?", sourceID, movieKeys).Delete(&model.MoviePlaylist{}).Error; err != nil {
				return err
			}
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "source_id"}, {Name: "movie_key"}, {Name: "group_index"}},
			DoUpdates: clause.AssignmentColumns([]string{"group_name", "content", "updated_at"}),
		}).Create(&playlists).Error; err != nil {
			return err
		}

		return nil
	})
}

func refreshLatestSourceStampByContentKeys(latestByContentKey map[string]int64) error {
	if len(latestByContentKey) == 0 {
		return nil
	}

	return db.Mdb.Transaction(func(tx *gorm.DB) error {
		for contentKey, stamp := range latestByContentKey {
			if stamp <= 0 {
				continue
			}
			if err := tx.Model(&model.SearchInfo{}).
				Where("content_key = ?", contentKey).
				Where("latest_source_stamp < ? OR latest_source_stamp IS NULL", stamp).
				Update("latest_source_stamp", stamp).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func refreshLatestSourceStampByMids(latestByMid map[int64]int64) error {
	if len(latestByMid) == 0 {
		return nil
	}

	return db.Mdb.Transaction(func(tx *gorm.DB) error {
		for mid, stamp := range latestByMid {
			if mid <= 0 || stamp <= 0 {
				continue
			}
			if err := tx.Model(&model.SearchInfo{}).
				Where("mid = ?", mid).
				Where("latest_source_stamp < ? OR latest_source_stamp IS NULL", stamp).
				Update("latest_source_stamp", stamp).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func mapKeys(m map[string]int64) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

// DeletePlaylistBySourceId 根据来源站点 ID 删除所有关联的播放列表资源
func DeletePlaylistBySourceId(sourceId string) error {
	return db.Mdb.Where("source_id = ?", sourceId).Delete(&model.MoviePlaylist{}).Error
}

func GetMovieDetailByDBID(mid int64, name string) []model.MoviePlaySource {
	var mps []model.MoviePlaySource
	lookupKeys := loadMovieMatchKeysByMids([]int64{mid})[mid]
	if len(lookupKeys) == 0 {
		return nil
	}
	sources := support.GetCollectSourceList()
	for _, s := range sources {
		if s.Grade != model.SlaveCollect || !s.State {
			continue
		}

		groups := GetMultiplePlayGroupsByKeys(s.Id, s.Name, lookupKeys)
		if len(groups) == 0 {
			continue
		}

		for _, item := range groups {
			if len(item.LinkList) > 0 {
				mps = append(mps, model.MoviePlaySource{SiteName: item.Name, PlayList: item.LinkList})
			}
		}
	}
	return mps
}

// CleanOrphanPlaylists 清理 movie_playlists 中与 search_infos 不匹配的孤儿记录
// 仅当 search_infos 存在数据时执行，避免主站清空后误删全部播放列表
func CleanOrphanPlaylists() int64 {
	var validKeys []string
	db.Mdb.Model(&model.MovieMatchKey{}).Distinct().Pluck("match_key", &validKeys)
	if len(validKeys) == 0 {
		log.Println("[CleanOrphan] movie_match_key 为空，跳过孤儿清理")
		return 0
	}
	validKeySet := make(map[string]struct{}, len(validKeys))
	for _, key := range validKeys {
		validKeySet[key] = struct{}{}
	}

	var allKeys []string
	db.Mdb.Model(&model.MoviePlaylist{}).Distinct().Pluck("movie_key", &allKeys)

	var orphanKeys []string
	for _, key := range allKeys {
		if _, ok := validKeySet[key]; !ok {
			orphanKeys = append(orphanKeys, key)
		}
	}

	if len(orphanKeys) == 0 {
		log.Println("[CleanOrphan] movie_playlists 无孤儿记录")
		return 0
	}

	const batchSize = 1000
	var total int64
	for i := 0; i < len(orphanKeys); i += batchSize {
		end := i + batchSize
		if end > len(orphanKeys) {
			end = len(orphanKeys)
		}
		result := db.Mdb.Where("movie_key IN ?", orphanKeys[i:end]).Delete(&model.MoviePlaylist{})
		total += result.RowsAffected
	}

	log.Printf("[CleanOrphan] 已清理 %d 条孤儿 movie_playlists 记录\n", total)
	return total
}

// GetMultiplePlay 通过影片名 hash 值匹配播放源
func GetMultiplePlay(siteId, key string) []model.MovieUrlInfo {
	return GetMultiplePlayByKeys(siteId, []string{key})
}

// GetMultiplePlayByKeys 按优先级批量匹配播放源，返回首个命中的播放列表
func GetMultiplePlayByKeys(siteId string, keys []string) []model.MovieUrlInfo {
	groups := GetMultiplePlayGroupsByKeys(siteId, "", keys)
	if len(groups) == 0 {
		return nil
	}
	return groups[0].LinkList
}

// GetMultiplePlayGroupsByKeys 按优先级批量匹配播放源，返回首个命中的完整播放组列表。
func GetMultiplePlayGroupsByKeys(siteId, siteName string, keys []string) []model.PlayLinkVo {
	orderedKeys := UniqueKeys(keys)
	if siteId == "" || len(orderedKeys) == 0 {
		return nil
	}

	var playlists []model.MoviePlaylist
	if err := db.Mdb.Where("source_id = ? AND movie_key IN ?", siteId, orderedKeys).
		Order("movie_key ASC").
		Order("group_index ASC").
		Find(&playlists).Error; err != nil {
		return nil
	}
	if len(playlists) == 0 {
		return nil
	}

	playlistByKey := make(map[string][]model.MoviePlaylist, len(playlists))
	for _, p := range playlists {
		playlistByKey[p.MovieKey] = append(playlistByKey[p.MovieKey], p)
	}
	for _, key := range orderedKeys {
		matched, ok := playlistByKey[key]
		if !ok {
			continue
		}

		groups := make([]model.PlayLinkVo, 0, len(matched))
		for _, playlist := range matched {
			var links []model.MovieUrlInfo
			if err := json.Unmarshal([]byte(playlist.Content), &links); err != nil || len(links) == 0 {
				continue
			}

			displayName := BuildDisplaySourceName(siteName, playlist.GroupName, playlist.GroupIndex, len(matched))
			groupID := BuildPlayGroupID(siteId, playlist.GroupName, playlist.GroupIndex, len(matched))
			groups = append(groups, model.PlayLinkVo{Id: groupID, SourceId: siteId, Name: displayName, LinkList: links})
		}
		if len(groups) > 0 {
			return groups
		}
	}
	return nil
}
