package film

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
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

	changedMovieKeys, err := saveGroupedPlaylists(sourceID, playlists, keysByMovieKey)
	if err != nil {
		log.Printf("SaveSitePlayList Error: %v", err)
		return err
	}
	if err := scheduleSearchInfoRefreshByPlaylists(sourceID, list, changedMovieKeys); err != nil {
		log.Printf("scheduleSearchInfoRefreshByPlaylists Error: %v", err)
		return err
	}
	if err := repository.TouchCollectSourceStatsTx(db.Mdb, sourceID, time.Now()); err != nil {
		log.Printf("TouchCollectSourceStats Error: %v", err)
		return err
	}

	return nil
}

func scheduleSearchInfoRefreshByPlaylists(sourceID string, details []model.MovieDetail, changedMovieKeys []string) error {
	infos, err := loadMatchedSearchInfosByDetails(details)
	if err != nil {
		return err
	}
	if err := saveSlaveSourceMappings(sourceID, details, infos); err != nil {
		return err
	}
	changedInfos, err := loadMatchedSearchInfosByMovieKeys(changedMovieKeys)
	if err != nil {
		return err
	}
	if err := touchSlavePlaylistUpdateStamps(changedInfos); err != nil {
		return err
	}
	SchedulePlaySummaryRefresh(infos...)
	return nil
}

func loadMatchedSearchInfosByDetails(details []model.MovieDetail) ([]model.FilmIndex, error) {
	type detailLookup struct {
		detail model.MovieDetail
		keys   []string
	}

	lookups := make([]detailLookup, 0, len(details))
	allKeys := make([]string, 0, len(details)*4)

	for _, detail := range details {
		lookupKeys := BuildPlaylistMovieKeys(detail)
		if len(lookupKeys) == 0 {
			continue
		}
		lookups = append(lookups, detailLookup{detail: detail, keys: lookupKeys})
		allKeys = append(allKeys, lookupKeys...)
	}

	if len(lookups) == 0 {
		return nil, nil
	}

	midsByLookupKey := loadMidCandidatesByMatchKeys(allKeys)
	matchedMidSet := make(map[int64]struct{}, len(allKeys))
	for _, mids := range midsByLookupKey {
		for _, mid := range mids {
			matchedMidSet[mid] = struct{}{}
		}
	}
	if len(matchedMidSet) == 0 {
		return nil, nil
	}

	matchedMids := make([]int64, 0, len(matchedMidSet))
	for mid := range matchedMidSet {
		matchedMids = append(matchedMids, mid)
	}

	var candidates []model.FilmIndex
	if err := db.Mdb.Where("mid IN ?", matchedMids).Find(&candidates).Error; err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	infoByMid := make(map[int64]model.FilmIndex, len(candidates))
	for _, info := range candidates {
		infoByMid[info.Mid] = info
	}

	ordered := make([]model.FilmIndex, 0, len(candidates))
	seenMid := make(map[int64]struct{}, len(candidates))
	for _, item := range lookups {
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
			if _, ok := seenMid[mid]; ok {
				continue
			}
			seenMid[mid] = struct{}{}
			ordered = append(ordered, infoByMid[mid])
		}
	}

	return ordered, nil
}

func loadMatchedSearchInfosByMovieKeys(movieKeys []string) ([]model.FilmIndex, error) {
	midsByLookupKey := loadMidCandidatesByMatchKeys(movieKeys)
	if len(midsByLookupKey) == 0 {
		return nil, nil
	}

	midSet := make(map[int64]struct{}, len(movieKeys))
	for _, mids := range midsByLookupKey {
		for _, mid := range mids {
			if mid > 0 {
				midSet[mid] = struct{}{}
			}
		}
	}
	if len(midSet) == 0 {
		return nil, nil
	}

	mids := make([]int64, 0, len(midSet))
	for mid := range midSet {
		mids = append(mids, mid)
	}

	var infos []model.FilmIndex
	if err := db.Mdb.Where("mid IN ?", mids).Find(&infos).Error; err != nil {
		return nil, err
	}
	return infos, nil
}

func touchSlavePlaylistUpdateStamps(infos []model.FilmIndex) error {
	mids := make([]int64, 0, len(infos))
	seen := make(map[int64]struct{}, len(infos))
	for _, info := range infos {
		if info.Mid <= 0 {
			continue
		}
		if _, ok := seen[info.Mid]; ok {
			continue
		}
		seen[info.Mid] = struct{}{}
		mids = append(mids, info.Mid)
	}
	if len(mids) == 0 {
		return nil
	}
	return db.Mdb.Model(&model.FilmIndex{}).Where("mid IN ?", mids).Update("update_stamp", time.Now().Unix()).Error
}

func saveGroupedPlaylists(sourceID string, playlists []model.MoviePlaylist, keysByMovieKey map[string]struct{}) ([]string, error) {
	movieKeys := make([]string, 0, len(keysByMovieKey))
	for movieKey := range keysByMovieKey {
		if strings.TrimSpace(movieKey) == "" {
			continue
		}
		movieKeys = append(movieKeys, movieKey)
	}

	var changedMovieKeys []string
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		existing, err := loadPlaylistSignaturesTx(tx, sourceID, movieKeys)
		if err != nil {
			return err
		}
		incoming := buildPlaylistSignatures(playlists)
		changedMovieKeys = diffPlaylistMovieKeys(existing, incoming, movieKeys)

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
	if err != nil {
		return nil, err
	}
	return changedMovieKeys, nil
}

type playlistSignature struct {
	GroupIndex int
	GroupName  string
	Content    string
}

func loadPlaylistSignaturesTx(tx *gorm.DB, sourceID string, movieKeys []string) (map[string][]playlistSignature, error) {
	result := make(map[string][]playlistSignature, len(movieKeys))
	if len(movieKeys) == 0 {
		return result, nil
	}
	var rows []model.MoviePlaylist
	if err := tx.Where("source_id = ? AND movie_key IN ?", sourceID, movieKeys).
		Order("movie_key ASC, group_index ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.MovieKey] = append(result[row.MovieKey], playlistSignature{
			GroupIndex: row.GroupIndex,
			GroupName:  row.GroupName,
			Content:    row.Content,
		})
	}
	return result, nil
}

func buildPlaylistSignatures(playlists []model.MoviePlaylist) map[string][]playlistSignature {
	result := make(map[string][]playlistSignature, len(playlists))
	for _, playlist := range playlists {
		result[playlist.MovieKey] = append(result[playlist.MovieKey], playlistSignature{
			GroupIndex: playlist.GroupIndex,
			GroupName:  playlist.GroupName,
			Content:    playlist.Content,
		})
	}
	return result
}

func diffPlaylistMovieKeys(existing map[string][]playlistSignature, incoming map[string][]playlistSignature, movieKeys []string) []string {
	changed := make([]string, 0, len(movieKeys))
	for _, movieKey := range movieKeys {
		if !samePlaylistSignatures(existing[movieKey], incoming[movieKey]) {
			changed = append(changed, movieKey)
		}
	}
	return changed
}

func samePlaylistSignatures(left []playlistSignature, right []playlistSignature) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func DeletePlaylistBySourceId(sourceID string) error {
	return DeletePlaylistBySourceIdTx(db.Mdb, sourceID)
}

func DeletePlaylistBySourceIdTx(tx *gorm.DB, sourceID string) error {
	return tx.Where("source_id = ?", sourceID).Delete(&model.MoviePlaylist{}).Error
}

// saveSlaveSourceMappings 为附属站播放列表补充 source_mid -> global_mid 映射，
// 让后台单片更新时能够按全局 mid 精确找到每个附属站自己的原始影片 ID。
func saveSlaveSourceMappings(sourceID string, details []model.MovieDetail, infos []model.FilmIndex) error {
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

	return saveMovieSourceMappingsTxE(db.Mdb, mappings)
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

func loadPlaylistsBySourceAndKeysTx(tx *gorm.DB, sourceIDs []string, keys []string) (map[string]map[string][]model.MoviePlaylist, error) {
	if len(sourceIDs) == 0 || len(keys) == 0 {
		return nil, nil
	}

	var playlists []model.MoviePlaylist
	if err := tx.Where("source_id IN ? AND movie_key IN ?", sourceIDs, keys).
		Order("source_id ASC").
		Order("movie_key ASC").
		Order("group_index ASC").
		Find(&playlists).Error; err != nil {
		return nil, err
	}

	result := make(map[string]map[string][]model.MoviePlaylist)
	for _, playlist := range playlists {
		byKey := result[playlist.SourceId]
		if byKey == nil {
			byKey = make(map[string][]model.MoviePlaylist)
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
	playlistsBySourceKey map[string]map[string][]model.MoviePlaylist,
) []model.PlayLinkVo {
	byKey := playlistsBySourceKey[siteID]
	if len(byKey) == 0 {
		return nil
	}
	for _, key := range UniqueKeys(keys) {
		matched := byKey[key]
		if len(matched) == 0 {
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
