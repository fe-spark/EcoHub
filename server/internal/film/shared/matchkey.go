package shared

import (
	"fmt"
	"sort"
	"strings"

	"server/internal/model"
	"server/internal/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BuildMovieMatchKeysWithCategory 跨站匹配键：豆瓣+片名、片名#大类、纯片名回退。
// 豆瓣 ID 相同但片名不同不算同一部，不能共用播放列表槽。
func BuildMovieMatchKeysWithCategory(dbID int64, name string, pid int64) []string {
	keys := make([]string, 0, 3)
	if dbIdentity := utils.BuildCollectionDbIdentity(dbID, name); dbIdentity != "" {
		keys = append(keys, utils.GenerateHashKey(dbIdentity))
	}
	normalizedTitle := utils.NormalizeIdentityTitle(name)
	if normalizedTitle != "" {
		if pid > 0 {
			keys = append(keys, utils.GenerateHashKey(fmt.Sprintf("%s#cat_%d", normalizedTitle, pid)))
		}
		keys = append(keys, utils.GenerateHashKey(normalizedTitle))
	}
	return UniqueKeys(keys)
}

func UniqueKeys(keys []string) []string {
	orderedKeys := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		orderedKeys = append(orderedKeys, k)
	}
	return orderedKeys
}

// LoadMovieMatchKeysByMidsTx 按 mid 批量读取匹配键（按主键升序）。
func LoadMovieMatchKeysByMidsTx(tx *gorm.DB, mids []int64) map[int64][]string {
	if len(mids) == 0 {
		return nil
	}

	var records []model.MovieMatchKey
	if err := tx.Where("mid IN ?", mids).Order("mid ASC, match_key ASC").Find(&records).Error; err != nil {
		return nil
	}

	result := make(map[int64][]string, len(mids))
	for _, record := range records {
		result[record.Mid] = append(result[record.Mid], record.MatchKey)
	}
	return result
}

func buildMovieMatchKeyRecords(mid int64, keys []string) []model.MovieMatchKey {
	keys = UniqueKeys(keys)
	records := make([]model.MovieMatchKey, 0, len(keys))
	for _, key := range keys {
		records = append(records, model.MovieMatchKey{Mid: mid, MatchKey: key})
	}
	return records
}

func SaveMovieMatchKeysByMidTx(tx *gorm.DB, midToKeys map[int64][]string) error {
	if len(midToKeys) == 0 {
		return nil
	}

	mids := make([]int64, 0, len(midToKeys))
	for mid := range midToKeys {
		if mid <= 0 {
			continue
		}
		mids = append(mids, mid)
	}
	if len(mids) == 0 {
		return nil
	}
	sort.Slice(mids, func(i, j int) bool { return mids[i] < mids[j] })

	existing := LoadMovieMatchKeysByMidsTx(tx, mids)
	have := make(map[int64]map[string]struct{}, len(existing))
	for mid, keys := range existing {
		set := make(map[string]struct{}, len(keys))
		for _, key := range keys {
			set[key] = struct{}{}
		}
		have[mid] = set
	}
	missing := make([]model.MovieMatchKey, 0)
	want := make(map[int64]map[string]struct{}, len(mids))
	for _, mid := range mids {
		for _, record := range buildMovieMatchKeyRecords(mid, midToKeys[mid]) {
			set := want[record.Mid]
			if set == nil {
				set = make(map[string]struct{})
				want[record.Mid] = set
			}
			set[record.MatchKey] = struct{}{}
			if _, ok := have[mid][record.MatchKey]; !ok {
				missing = append(missing, record)
			}
		}
	}
	if len(missing) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&missing).Error; err != nil {
			return err
		}
	}
	for mid, keys := range existing {
		kept := want[mid]
		for _, key := range keys {
			if _, ok := kept[key]; ok {
				continue
			}
			if err := tx.Where("mid = ? AND match_key = ?", mid, key).Delete(&model.MovieMatchKey{}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
