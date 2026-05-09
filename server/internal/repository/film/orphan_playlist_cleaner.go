package film

import (
	"log"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
)

const (
	orphanPlaylistScanBatchSize = 50000
	orphanPlaylistBatchCooldown = 5 * time.Millisecond
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
		rangeStart := lastID
		rangeEnd, ok, err := loadPlaylistCandidateRange(rangeStart)
		if err != nil {
			return total, err
		}
		if !ok {
			break
		}
		lastID = rangeEnd

		deleted, err := deleteOrphanPlaylistsInRange(rangeStart, rangeEnd)
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

func loadPlaylistCandidateRange(lastID uint) (uint, bool, error) {
	var rows []playlistCandidateRow
	err := db.Mdb.Model(&model.MoviePlaylist{}).
		Select("movie_playlist.id").
		Joins("JOIN film_sources ON film_sources.id = movie_playlist.source_id AND film_sources.grade = ?", model.SlaveCollect).
		Where("movie_playlist.id > ?", lastID).
		Order("movie_playlist.id ASC").
		Limit(orphanPlaylistScanBatchSize).
		Scan(&rows).Error
	if err != nil {
		return 0, false, err
	}
	if len(rows) == 0 {
		return 0, false, nil
	}
	return rows[len(rows)-1].ID, true, nil
}

func deleteOrphanPlaylistsInRange(rangeStart uint, rangeEnd uint) (int64, error) {
	if rangeEnd <= rangeStart {
		return 0, nil
	}
	result := db.Mdb.Exec(`
		DELETE movie_playlist
		FROM movie_playlist
		JOIN film_sources ON film_sources.id = movie_playlist.source_id AND film_sources.grade = ?
		LEFT JOIN movie_match_key ON movie_match_key.match_key = movie_playlist.movie_key
		WHERE movie_playlist.id > ?
			AND movie_playlist.id <= ?
			AND movie_playlist.deleted_at IS NULL
			AND movie_match_key.id IS NULL
	`, model.SlaveCollect, rangeStart, rangeEnd)
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
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
