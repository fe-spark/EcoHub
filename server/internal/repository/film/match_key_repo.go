package film

import (
	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func buildMovieMatchKeyRecords(mid int64, keys []string) []model.MovieMatchKey {
	keys = UniqueKeys(keys)
	records := make([]model.MovieMatchKey, 0, len(keys))
	for _, key := range keys {
		records = append(records, model.MovieMatchKey{Mid: mid, MatchKey: key})
	}
	return records
}

func saveMovieMatchKeysByMid(midToKeys map[int64][]string) error {
	if err := saveMovieMatchKeysByMidTx(db.Mdb, midToKeys); err != nil {
		return err
	}
	var allKeys []string
	for _, keys := range midToKeys {
		allKeys = append(allKeys, keys...)
	}
	if len(allKeys) > 0 {
		return ReviveSlavePlaylistsTx(db.Mdb, allKeys)
	}
	return nil
}

func saveMovieMatchKeysByMidTx(tx *gorm.DB, midToKeys map[int64][]string) error {
	if len(midToKeys) == 0 {
		return nil
	}

	mids := make([]int64, 0, len(midToKeys))
	records := make([]model.MovieMatchKey, 0, len(midToKeys)*4)
	for mid, keys := range midToKeys {
		if mid <= 0 {
			continue
		}
		mids = append(mids, mid)
		records = append(records, buildMovieMatchKeyRecords(mid, keys)...)
	}
	if len(mids) == 0 {
		return nil
	}

	if err := tx.Unscoped().Where("mid IN ?", mids).Delete(&model.MovieMatchKey{}).Error; err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&records).Error
}

// ReviveSlavePlaylistsTx 主站写入或重试恢复生成匹配键时，即时批量复活可能处于观察期的附属站播放列表。
// 内部包含 Fast-path 探针：先检测是否存在处于观察期的记录，无记录时直接跳过，杜绝无意义的排他写锁。
func ReviveSlavePlaylistsTx(tx *gorm.DB, matchKeys []string) error {
	matchKeys = UniqueKeys(matchKeys)
	if len(matchKeys) == 0 {
		return nil
	}
	if tx == nil {
		tx = db.Mdb
	}
	if tx == nil {
		return nil
	}
	const batchSize = 500
	for i := 0; i < len(matchKeys); i += batchSize {
		end := i + batchSize
		if end > len(matchKeys) {
			end = len(matchKeys)
		}
		chunk := matchKeys[i:end]

		// Fast-path 探针：先查是否有处于观察期的行，避免无意义的排他锁与死锁
		var dummy uint
		res := tx.Model(&model.SlaveMoviePlaylist{}).
			Select("id").
			Where("movie_key IN ? AND orphan_marked_at IS NOT NULL", chunk).
			Limit(1).
			Scan(&dummy)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			continue
		}

		if err := tx.Model(&model.SlaveMoviePlaylist{}).
			Where("movie_key IN ? AND orphan_marked_at IS NOT NULL", chunk).
			Update("orphan_marked_at", gorm.Expr("NULL")).Error; err != nil {
			return err
		}
	}
	return nil
}

func deleteMovieMatchKeysByMids(tx *gorm.DB, mids []int64) error {
	if len(mids) == 0 {
		return nil
	}
	return tx.Where("mid IN ?", mids).Delete(&model.MovieMatchKey{}).Error
}

func loadMovieMatchKeysByMids(mids []int64) map[int64][]string {
	return loadMovieMatchKeysByMidsTx(db.Mdb, mids)
}

func loadMovieMatchKeysByMidsTx(tx *gorm.DB, mids []int64) map[int64][]string {
	if len(mids) == 0 {
		return nil
	}

	var records []model.MovieMatchKey
	if err := tx.Where("mid IN ?", mids).Order("id ASC").Find(&records).Error; err != nil {
		return nil
	}

	result := make(map[int64][]string, len(mids))
	for _, record := range records {
		result[record.Mid] = append(result[record.Mid], record.MatchKey)
	}
	return result
}

func loadMidCandidatesByMatchKeys(keys []string) map[string][]int64 {
	keys = UniqueKeys(keys)
	if len(keys) == 0 {
		return nil
	}

	var records []model.MovieMatchKey
	if err := db.Mdb.Where("match_key IN ?", keys).Order("id ASC").Find(&records).Error; err != nil {
		return nil
	}

	result := make(map[string][]int64, len(keys))
	for _, record := range records {
		result[record.MatchKey] = append(result[record.MatchKey], record.Mid)
	}
	return result
}

func LoadMovieMatchKeys(filmIndex *model.FilmIndex, detail *model.MovieDetail) []string {
	if filmIndex != nil && filmIndex.Mid > 0 {
		if keys := loadMovieMatchKeysByMids([]int64{filmIndex.Mid})[filmIndex.Mid]; len(keys) > 0 {
			return keys
		}
	}
	if detail == nil {
		return nil
	}
	return BuildMovieMatchKeys(detail.DbId, detail.Name)
}

func LoadMovieMatchKeysBySnapshot(snapshot *model.FilmListSnapshot, detail *model.MovieDetail) []string {
	if snapshot != nil && snapshot.Mid > 0 {
		if keys := loadMovieMatchKeysByMids([]int64{snapshot.Mid})[snapshot.Mid]; len(keys) > 0 {
			return keys
		}
	}
	if detail == nil {
		return nil
	}
	return BuildMovieMatchKeys(detail.DbId, detail.Name)
}
