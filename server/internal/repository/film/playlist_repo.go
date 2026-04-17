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
	latestByContentKey := make(map[string]int64)
forLoop:
	for _, d := range list {
		if len(d.PlayList) == 0 || strings.Contains(d.CName, "解说") {
			continue forLoop
		}

		data, _ := json.Marshal(d.PlayList)
		stamp, err := time.ParseInLocation(time.DateTime, d.UpdateTime, time.Local)
		if err == nil {
			contentKey := BuildContentKey(d)
			if contentKey != "" && stamp.Unix() > latestByContentKey[contentKey] {
				latestByContentKey[contentKey] = stamp.Unix()
			}
		}

		for _, movieKey := range BuildPlaylistMovieKeys(d) {
			playlists = append(playlists, model.MoviePlaylist{
				SourceId: id,
				MovieKey: movieKey,
				Content:  string(data),
			})
		}
	}

	if len(playlists) == 0 {
		return nil
	}

	if err := db.Mdb.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source_id"}, {Name: "movie_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"content", "updated_at"}),
	}).Create(&playlists).Error; err != nil {
		log.Printf("SaveSitePlayList Error: %v", err)
		return err
	}

	if err := refreshLatestSourceStampByContentKeys(latestByContentKey); err != nil {
		log.Printf("refreshLatestSourceStampByContentKeys Error: %v", err)
		return err
	}

	log.Printf("[Playlist] 为站点 %s 保存了 %d 条记录\n", id, len(playlists))
	return nil
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

// DeletePlaylistBySourceId 根据来源站点 ID 删除所有关联的播放列表资源
func DeletePlaylistBySourceId(sourceId string) error {
	return db.Mdb.Where("source_id = ?", sourceId).Delete(&model.MoviePlaylist{}).Error
}

func GetMovieDetailByDBID(mid int64, name string) []model.MoviePlaySource {
	var mps []model.MoviePlaySource
	sources := support.GetCollectSourceList()
	for _, s := range sources {
		if s.Grade != model.SlaveCollect || !s.State {
			continue
		}

		lookupKeys := BuildMovieLookupKeys(mid, name)
		if len(lookupKeys) == 0 {
			continue
		}

		var playlist model.MoviePlaylist
		if err := db.Mdb.Where("source_id = ? AND movie_key = ?", s.Id, lookupKeys[0]).First(&playlist).Error; err != nil && len(lookupKeys) > 1 {
			db.Mdb.Where("source_id = ? AND movie_key = ?", s.Id, lookupKeys[1]).First(&playlist)
		}
		if playlist.ID == 0 {
			continue
		}

		var playLists [][]model.MovieUrlInfo
		if jsonErr := json.Unmarshal([]byte(playlist.Content), &playLists); jsonErr != nil {
			continue
		}
		for _, pl := range playLists {
			if len(pl) > 0 {
				mps = append(mps, model.MoviePlaySource{SiteName: s.Name, PlayList: pl})
			}
		}
	}
	return mps
}

// CleanOrphanPlaylists 清理 movie_playlists 中与 search_infos 不匹配的孤儿记录
// 仅当 search_infos 存在数据时执行，避免主站清空后误删全部播放列表
func CleanOrphanPlaylists() int64 {
	var films []struct {
		Name string
		DbId int64
	}
	db.Mdb.Model(&model.SearchInfo{}).Select("name", "db_id").Scan(&films)
	if len(films) == 0 {
		log.Println("[CleanOrphan] search_infos 为空，跳过孤儿清理")
		return 0
	}

	validKeys := BuildValidPlaylistKeys(films)

	var allKeys []string
	db.Mdb.Model(&model.MoviePlaylist{}).Distinct().Pluck("movie_key", &allKeys)

	var orphanKeys []string
	for _, key := range allKeys {
		if _, ok := validKeys[key]; !ok {
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
	orderedKeys := UniqueKeys(keys)
	if siteId == "" || len(orderedKeys) == 0 {
		return nil
	}

	var playlists []model.MoviePlaylist
	if err := db.Mdb.Where("source_id = ? AND movie_key IN ?", siteId, orderedKeys).Find(&playlists).Error; err != nil {
		return nil
	}
	if len(playlists) == 0 {
		return nil
	}

	contentByKey := make(map[string]string, len(playlists))
	for _, p := range playlists {
		contentByKey[p.MovieKey] = p.Content
	}

	return ExtractFirstPlayableList(contentByKey, orderedKeys)
}
