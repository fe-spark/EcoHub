package film

import (
	"log"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
)

const (
	orphanPlaylistScanBatchSize   = 5000
	orphanPlaylistDeleteBatchSize = 500
	orphanPlaylistBatchCooldown   = 20 * time.Millisecond
)

type orphanPlaylistRow struct {
	ID uint
}

type playlistCandidateRow struct {
	ID uint
}

// CleanOrphanPlaylists 清理与主站匹配键索引脱离关联的附属站播放列表。
func CleanOrphanPlaylists() (int64, error) {
	total, err := cleanOrphanPlaylistsInBatches()
	if err != nil {
		return total, err
	}
	if total > 0 {
		log.Printf("[CleanOrphan] 已清理 %d 条孤儿 movie_playlist 记录", total)
	}
	return total, nil
}

func cleanOrphanPlaylistsInBatches() (int64, error) {
	if hasKeys, err := hasMovieMatchKeys(); err != nil {
		return 0, err
	} else if !hasKeys {
		log.Println("[CleanOrphan] movie_match_key 为空，跳过孤儿清理")
		return 0, nil
	}

	var total int64
	var lastID uint
	for {
		candidates, err := loadPlaylistCandidateRows(lastID)
		if err != nil {
			return total, err
		}
		if len(candidates) == 0 {
			break
		}

		candidateIDs := make([]uint, 0, len(candidates))
		for _, row := range candidates {
			candidateIDs = append(candidateIDs, row.ID)
			if row.ID > lastID {
				lastID = row.ID
			}
		}

		ids, err := loadOrphanPlaylistIDs(candidateIDs)
		if err != nil {
			return total, err
		}
		if len(ids) == 0 {
			time.Sleep(orphanPlaylistBatchCooldown)
			continue
		}

		deleted, err := deleteOrphanPlaylistsByIDs(ids)
		if err != nil {
			return total, err
		}
		total += deleted
		if deleted > 0 {
			log.Printf("[CleanOrphan] 分批清理进度 deleted=%d total=%d last_id=%d", deleted, total, lastID)
		}
		time.Sleep(orphanPlaylistBatchCooldown)
	}
	if total == 0 {
		log.Println("[CleanOrphan] movie_playlist 无孤儿记录")
	}
	return total, nil
}

func hasMovieMatchKeys() (bool, error) {
	var row orphanPlaylistRow
	if err := db.Mdb.Model(&model.MovieMatchKey{}).Select("id").Limit(1).Scan(&row).Error; err != nil {
		return false, err
	}
	return row.ID > 0, nil
}

func loadPlaylistCandidateRows(lastID uint) ([]playlistCandidateRow, error) {
	var rows []playlistCandidateRow
	err := db.Mdb.Model(&model.MoviePlaylist{}).
		Select("movie_playlist.id").
		Joins("JOIN film_sources ON film_sources.id = movie_playlist.source_id AND film_sources.grade = ?", model.SlaveCollect).
		Where("movie_playlist.id > ?", lastID).
		Order("movie_playlist.id ASC").
		Limit(orphanPlaylistScanBatchSize).
		Scan(&rows).Error
	return rows, err
}

func loadOrphanPlaylistIDs(candidateIDs []uint) ([]uint, error) {
	if len(candidateIDs) == 0 {
		return nil, nil
	}
	var rows []orphanPlaylistRow
	err := db.Mdb.Model(&model.MoviePlaylist{}).
		Select("movie_playlist.id").
		Joins("LEFT JOIN movie_match_key ON movie_match_key.match_key = movie_playlist.movie_key").
		Where("movie_playlist.id IN ?", candidateIDs).
		Where("movie_match_key.id IS NULL").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids, nil
}

func deleteOrphanPlaylistsByIDs(ids []uint) (int64, error) {
	var total int64
	for i := 0; i < len(ids); i += orphanPlaylistDeleteBatchSize {
		end := i + orphanPlaylistDeleteBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		result := db.Mdb.Where("id IN ?", ids[i:end]).Delete(&model.MoviePlaylist{})
		if result.Error != nil {
			return total, result.Error
		}
		total += result.RowsAffected
	}
	return total, nil
}

func RefreshAfterDataClean() error {
	var infos []model.FilmIndex
	if err := db.Mdb.Find(&infos).Error; err != nil {
		return err
	}
	if err := RefreshPlayFromSummaryByIndexes(infos); err != nil {
		return err
	}
	return ActivateRebuiltFilmListSnapshot(NewSnapshotVersion())
}
