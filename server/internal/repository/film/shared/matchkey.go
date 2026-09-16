package shared

import (
	"fmt"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"
	"server/internal/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BuildMovieMatchKeysWithCategory 跨站匹配键：豆瓣、片名#大类、纯片名回退。同名跨类不能共用播放列表槽。
func BuildMovieMatchKeysWithCategory(dbID int64, name string, pid int64) []string {
	keys := make([]string, 0, 3)
	if dbIdentity := utils.BuildCollectionDbIdentity(dbID, name); dbIdentity != "" {
		keys = append(keys, utils.GenerateHashKey(dbIdentity))
	}
	normalizedTitle := utils.NormalizeCollectionTitle(name)
	if normalizedTitle != "" {
		if pid > 0 {
			keys = append(keys, utils.GenerateHashKey(fmt.Sprintf("%s#cat_%d", normalizedTitle, pid)))
		}
		keys = append(keys, utils.GenerateHashKey(normalizedTitle))
	}
	return UniqueKeys(keys)
}
func BuildMovieMatchKeys(dbID int64, name string) []string {
	return BuildMovieMatchKeysWithCategory(dbID, name, 0)
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

// LoadMovieMatchKeysByMidsTx 按 mid 批量读取匹配键（按 id 升序，保持写入顺序）。
func LoadMovieMatchKeysByMidsTx(tx *gorm.DB, mids []int64) map[int64][]string {
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

func buildMovieMatchKeyRecords(mid int64, keys []string) []model.MovieMatchKey {
	keys = UniqueKeys(keys)
	records := make([]model.MovieMatchKey, 0, len(keys))
	for _, key := range keys {
		records = append(records, model.MovieMatchKey{Mid: mid, MatchKey: key})
	}
	return records
}

func SaveMovieMatchKeysByMid(midToKeys map[int64][]string) error {
	return SaveMovieMatchKeysByMidTx(db.Mdb, midToKeys)
}

func SaveMovieMatchKeysByMidTx(tx *gorm.DB, midToKeys map[int64][]string) error {
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

func LoadMovieMatchKeysByMids(mids []int64) map[int64][]string {
	return LoadMovieMatchKeysByMidsTx(db.Mdb, mids)
}

func LoadMidCandidatesByMatchKeys(keys []string) map[string][]int64 {
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

func LoadMovieMatchKeysBySnapshot(snapshot *model.FilmListSnapshot, detail *model.MovieDetail) []string {
	if snapshot != nil && snapshot.Mid > 0 {
		if keys := LoadMovieMatchKeysByMids([]int64{snapshot.Mid})[snapshot.Mid]; len(keys) > 0 {
			return keys
		}
	}
	if detail == nil {
		return nil
	}
	pid := int64(0)
	if snapshot != nil {
		if snapshot.Pid > 0 {
			pid = support.GetRootId(snapshot.Pid)
		}
		if pid <= 0 && snapshot.Cid > 0 {
			pid = support.GetRootId(snapshot.Cid)
		}
	}
	if pid <= 0 {
		pid = ResolveMovieDetailRootPid(*detail)
	}
	return BuildMovieMatchKeysWithCategory(detail.DbId, detail.Name, pid)
}

func BuildPlaylistMovieKeys(detail model.MovieDetail) []string {
	pid := ResolveMovieDetailRootPid(detail)
	return BuildMovieMatchKeysWithCategory(detail.DbId, detail.Name, pid)
}

// BuildPlaylistPrimaryMovieKey 播放列表 / 海报主键（豆瓣，否则片名#大类，否则纯片名）。
func BuildPlaylistPrimaryMovieKey(detail model.MovieDetail) string {
	keys := BuildPlaylistMovieKeys(detail)
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

// ResolveSlaveGlobalMid 用影片身份键把详情翻译成全局 mid。
func ResolveSlaveGlobalMid(detail model.MovieDetail, globalMidByKey map[string]int64) (int64, bool) {
	for _, key := range BuildPlaylistMovieKeys(detail) {
		globalMid, ok := globalMidByKey[key]
		if ok {
			return globalMid, true
		}
	}
	return 0, false
}
