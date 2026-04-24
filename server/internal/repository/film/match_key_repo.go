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
	return saveMovieMatchKeysByMidTx(db.Mdb, midToKeys)
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

func LoadMovieMatchKeys(search *model.SearchInfo, detail *model.MovieDetail) []string {
	if search != nil && search.Mid > 0 {
		if keys := loadMovieMatchKeysByMids([]int64{search.Mid})[search.Mid]; len(keys) > 0 {
			return keys
		}
	}
	if detail == nil {
		return nil
	}
	return BuildMovieMatchKeys(detail.DbId, detail.Name)
}
