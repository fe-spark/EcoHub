package film

import (
	"log"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
)

const (
	orphanPlaylistScanBatchSize   = 5000
	orphanPlaylistDeleteBatchSize = 200
	orphanPlaylistBatchCooldown   = 5 * time.Millisecond
)

type orphanPlaylistRow struct {
	ID       uint
	MovieKey string
}

type playlistCandidateRow struct {
	ID uint
}

type matchKeyRow struct {
	MatchKey string
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
		loadStartedAt := time.Now()
		rangeEnd, ok, err := loadPlaylistCandidateRange(rangeStart)
		if err != nil {
			return total, err
		}
		if !ok {
			break
		}
		lastID = rangeEnd
		log.Printf("[CleanOrphan] 分批扫描区间 range=(%d,%d] cost=%s", rangeStart, rangeEnd, time.Since(loadStartedAt))

		startedAt := time.Now()
		ids, err := loadOrphanPlaylistIDsInRange(rangeStart, rangeEnd)
		if err != nil {
			return total, err
		}
		deleted, err := deleteOrphanPlaylistsByIDs(ids)
		if err != nil {
			return total, err
		}
		total += deleted
		log.Printf("[CleanOrphan] 分批清理进度 range=(%d,%d] orphan=%d deleted=%d total=%d cost=%s", rangeStart, rangeEnd, len(ids), deleted, total, time.Since(startedAt))
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

func loadOrphanPlaylistIDsInRange(rangeStart uint, rangeEnd uint) ([]uint, error) {
	if rangeEnd <= rangeStart {
		return nil, nil
	}
	var rows []orphanPlaylistRow
	err := db.Mdb.Model(&model.MoviePlaylist{}).
		Select("movie_playlist.id, movie_playlist.movie_key").
		Joins("JOIN film_sources ON film_sources.id = movie_playlist.source_id AND film_sources.grade = ?", model.SlaveCollect).
		Where("movie_playlist.id > ? AND movie_playlist.id <= ?", rangeStart, rangeEnd).
		Where("movie_playlist.deleted_at IS NULL").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	keys := make([]string, 0, len(rows))
	seenKeys := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.MovieKey == "" {
			continue
		}
		if _, ok := seenKeys[row.MovieKey]; ok {
			continue
		}
		seenKeys[row.MovieKey] = struct{}{}
		keys = append(keys, row.MovieKey)
	}
	existingKeys, err := loadExistingMatchKeySet(keys)
	if err != nil {
		return nil, err
	}

	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		if row.MovieKey == "" {
			ids = append(ids, row.ID)
			continue
		}
		if _, ok := existingKeys[row.MovieKey]; !ok {
			ids = append(ids, row.ID)
		}
	}
	return ids, nil
}

func loadExistingMatchKeySet(keys []string) (map[string]struct{}, error) {
	existing := make(map[string]struct{}, len(keys))
	if len(keys) == 0 {
		return existing, nil
	}
	var rows []matchKeyRow
	if err := db.Mdb.Model(&model.MovieMatchKey{}).
		Select("match_key").
		Where("match_key IN ?", keys).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		existing[row.MatchKey] = struct{}{}
	}
	return existing, nil
}

func deleteOrphanPlaylistsByIDs(ids []uint) (int64, error) {
	var total int64
	for i := 0; i < len(ids); i += orphanPlaylistDeleteBatchSize {
		end := i + orphanPlaylistDeleteBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		result := db.Mdb.Unscoped().Where("id IN ?", ids[i:end]).Delete(&model.MoviePlaylist{})
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
