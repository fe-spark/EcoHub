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

func SaveSitePlayList(sourceID string, list []model.MovieDetail) error {
	if len(list) == 0 {
		return nil
	}

	var playlists []model.MoviePlaylist
	keysByMovieKey := make(map[string]struct{}, len(list)*2)

	for _, detail := range list {
		if len(detail.PlayList) == 0 || strings.Contains(detail.CName, "解说") {
			continue
		}

		keys := BuildPlaylistMovieKeys(detail)
		for _, movieKey := range keys {
			keysByMovieKey[movieKey] = struct{}{}
		}

		for _, movieKey := range keys {
			for index, links := range detail.PlayList {
				if len(links) == 0 {
					continue
				}

				data, _ := json.Marshal(links)
				rawName := ""
				if index < len(detail.PlayFrom) {
					rawName = strings.TrimSpace(detail.PlayFrom[index])
				}

				playlists = append(playlists, model.MoviePlaylist{
					SourceId:   sourceID,
					MovieKey:   movieKey,
					GroupIndex: index,
					GroupName:  rawName,
					Content:    string(data),
				})
			}
		}
	}

	if len(keysByMovieKey) == 0 {
		return nil
	}

	if err := saveGroupedPlaylists(sourceID, playlists, keysByMovieKey); err != nil {
		log.Printf("SaveSitePlayList Error: %v", err)
		return err
	}

	if err := refreshSearchInfosByPlaylists(sourceID, list); err != nil {
		log.Printf("refreshSearchInfosByPlaylists Error: %v", err)
		return err
	}

	log.Printf("[Playlist] 为站点 %s 保存了 %d 条记录\n", sourceID, len(playlists))
	return nil
}

func refreshSearchInfosByPlaylists(sourceID string, details []model.MovieDetail) error {
	infos, latestByMid, err := loadMatchedSearchInfosByDetails(details)
	if err != nil {
		return err
	}
	if err := refreshLatestSourceStampByMids(latestByMid); err != nil {
		return err
	}
	if err := saveSlaveSourceMappings(sourceID, details, infos); err != nil {
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
			if err := tx.Unscoped().
				Where("source_id = ? AND movie_key IN ?", sourceID, movieKeys).
				Delete(&model.MoviePlaylist{}).Error; err != nil {
				return err
			}
		}

		if len(playlists) > 0 {
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "source_id"}, {Name: "movie_key"}, {Name: "group_index"}},
				DoUpdates: clause.AssignmentColumns([]string{"group_name", "content", "updated_at", "deleted_at"}),
			}).Create(&playlists).Error; err != nil {
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

	updatedPids := make(map[int64]struct{}, len(latestByMid))
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		for mid, stamp := range latestByMid {
			if mid <= 0 || stamp <= 0 {
				continue
			}
			var info model.SearchInfo
			if err := tx.Select("pid").Where("mid = ?", mid).First(&info).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.SearchInfo{}).
				Where("mid = ?", mid).
				Where("latest_source_stamp < ? OR latest_source_stamp IS NULL", stamp).
				Update("latest_source_stamp", stamp).Error; err != nil {
				return err
			}
			if info.Pid > 0 {
				updatedPids[info.Pid] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	clearSearchInfoCachesByPidSet(updatedPids)
	return nil
}

func DeletePlaylistBySourceId(sourceID string) error {
	return DeletePlaylistBySourceIdTx(db.Mdb, sourceID)
}

func DeletePlaylistBySourceIdTx(tx *gorm.DB, sourceID string) error {
	return tx.Where("source_id = ?", sourceID).Delete(&model.MoviePlaylist{}).Error
}

// saveSlaveSourceMappings 为附属站播放列表补充 source_mid -> global_mid 映射，
// 让后台单片更新时能够按全局 mid 精确找到每个附属站自己的原始影片 ID。
func saveSlaveSourceMappings(sourceID string, details []model.MovieDetail, infos []model.SearchInfo) error {
	if len(details) == 0 || len(infos) == 0 {
		return nil
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil
	}

	mids := make([]int64, 0, len(infos))
	for _, info := range infos {
		if info.Mid > 0 {
			mids = append(mids, info.Mid)
		}
	}
	if len(mids) == 0 {
		return nil
	}

	globalMidByKey := make(map[string]int64, len(mids)*2)
	keysByMid := loadMovieMatchKeysByMids(mids)
	for mid, keys := range keysByMid {
		for _, key := range keys {
			if strings.TrimSpace(key) == "" {
				continue
			}
			globalMidByKey[key] = mid
		}
	}
	if len(globalMidByKey) == 0 {
		return nil
	}

	mappings := make([]model.MovieSourceMapping, 0, len(details))
	for _, detail := range details {
		if detail.Id <= 0 {
			continue
		}
		globalMid, ok := resolveSlaveGlobalMid(detail, globalMidByKey)
		if !ok || globalMid <= 0 {
			continue
		}
		mappings = append(mappings, model.MovieSourceMapping{
			SourceId:  sourceID,
			SourceMid: detail.Id,
			GlobalMid: globalMid,
		})
	}

	saveMovieSourceMappings(mappings)
	return nil
}

func resolveSlaveGlobalMid(detail model.MovieDetail, globalMidByKey map[string]int64) (int64, bool) {
	for _, key := range BuildPlaylistMovieKeys(detail) {
		globalMid, ok := globalMidByKey[key]
		if ok {
			return globalMid, true
		}
	}
	return 0, false
}

func GetMultiplePlayGroupsByKeys(siteID, siteName string, keys []string) []model.PlayLinkVo {
	return getMultiplePlayGroupsByKeysTx(db.Mdb, siteID, siteName, keys)
}

func getMultiplePlayGroupsByKeysTx(tx *gorm.DB, siteID, siteName string, keys []string) []model.PlayLinkVo {
	orderedKeys := UniqueKeys(keys)
	if siteID == "" || len(orderedKeys) == 0 {
		return nil
	}

	var playlists []model.MoviePlaylist
	if err := tx.Where("source_id = ? AND movie_key IN ?", siteID, orderedKeys).
		Order("movie_key ASC").
		Order("group_index ASC").
		Find(&playlists).Error; err != nil {
		return nil
	}
	if len(playlists) == 0 {
		return nil
	}

	playlistByKey := make(map[string][]model.MoviePlaylist, len(playlists))
	for _, playlist := range playlists {
		playlistByKey[playlist.MovieKey] = append(playlistByKey[playlist.MovieKey], playlist)
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
			groupID := BuildPlayGroupID(siteID, playlist.GroupName, playlist.GroupIndex, len(matched))
			groups = append(groups, model.PlayLinkVo{
				Id:       groupID,
				SourceId: siteID,
				Name:     displayName,
				LinkList: links,
			})
		}
		if len(groups) > 0 {
			return groups
		}
	}

	return nil
}

func loadPlaylistGroupsByInfos(infos []model.SearchInfo) (map[int64]map[string][]model.PlayLinkVo, error) {
	return loadPlaylistGroupsByInfosTx(db.Mdb, infos)
}

func loadPlaylistGroupsByInfosTx(tx *gorm.DB, infos []model.SearchInfo) (map[int64]map[string][]model.PlayLinkVo, error) {
	result := make(map[int64]map[string][]model.PlayLinkVo, len(infos))
	mids := make([]int64, 0, len(infos))
	for _, info := range infos {
		if info.Mid > 0 {
			mids = append(mids, info.Mid)
		}
	}

	keysByMid := loadMovieMatchKeysByMidsTx(tx, mids)
	for _, info := range infos {
		groupsBySource := make(map[string][]model.PlayLinkVo)
		lookupKeys := keysByMid[info.Mid]
		if len(lookupKeys) == 0 {
			result[info.Mid] = groupsBySource
			continue
		}

		for _, source := range support.GetCollectSourceList() {
			if source.Grade != model.SlaveCollect || !source.State {
				continue
			}
			groups := getMultiplePlayGroupsByKeysTx(tx, source.Id, source.Name, lookupKeys)
			if len(groups) == 0 {
				continue
			}
			groupsBySource[source.Id] = groups
		}
		result[info.Mid] = groupsBySource
	}
	return result, nil
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

// CleanOrphanPlaylists 清理与主站匹配键索引脱离关联的附属站播放列表。
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
		log.Println("[CleanOrphan] movie_playlist 无孤儿记录")
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

	log.Printf("[CleanOrphan] 已清理 %d 条孤儿 movie_playlist 记录\n", total)
	return total
}
