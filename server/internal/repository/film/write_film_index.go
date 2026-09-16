package film

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var movieSourceMappingWriteMu sync.Mutex

// filmIndexMidUpsert 按 mid 冲突更新（主站 mid = 源站 vod_id）。
// 旧库存 content_key=name_* 时仍能命中同一行，并在更新时懒升 content_key→vod_*。
func filmIndexMidUpsert() clause.OnConflict {
	return clause.OnConflict{
		Columns:   []clause.Column{{Name: "mid"}},
		DoUpdates: clause.AssignmentColumns(filmIndexUpsertUpdateColumns),
	}
}

func movieSourceMappingUpsert() clause.OnConflict {
	return clause.OnConflict{
		Columns:   []clause.Column{{Name: "source_id"}, {Name: "source_mid"}},
		DoUpdates: clause.AssignmentColumns([]string{"global_mid", "updated_at", "deleted_at"}),
	}
}

func filterValidFilmIndexes(list []model.FilmIndex) []model.FilmIndex {
	validList := make([]model.FilmIndex, 0, len(list))
	for _, item := range list {
		if strings.TrimSpace(item.Name) == "" {
			continue
		}
		validList = append(validList, item)
	}
	return validList
}

func upsertFilmIndexes(list []model.FilmIndex) error {
	return upsertFilmIndexesTx(db.Mdb, list)
}

func upsertFilmIndexesTx(tx *gorm.DB, list []model.FilmIndex) error {
	if len(list) == 0 {
		return nil
	}
	// 软删占键可安全释放；活跃行占键则报错，避免生产误改他片身份键。
	if err := releaseConflictingContentKeysTx(tx, list); err != nil {
		return err
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Mid < list[j].Mid
	})
	return tx.Clauses(filmIndexMidUpsert()).CreateInBatches(&list, upsertBatchSize).Error
}

// releaseConflictingContentKeysTx 处理 content_key=目标 且 mid≠本批归属 的占用行：
//   - 软删行：改名为 del_{id}，释放唯一键（常见、安全）；
//   - 活跃行：直接报错，不自动抢键（脏数据需人工处理，避免写坏库存）。
func releaseConflictingContentKeysTx(tx *gorm.DB, list []model.FilmIndex) error {
	for _, item := range list {
		key := strings.TrimSpace(item.ContentKey)
		if key == "" || item.Mid <= 0 {
			continue
		}
		var occupants []model.FilmIndex
		if err := tx.Unscoped().Model(&model.FilmIndex{}).
			Select("id", "mid", "deleted_at").
			Where("content_key = ? AND mid <> ?", key, item.Mid).
			Find(&occupants).Error; err != nil {
			return err
		}
		for _, o := range occupants {
			if !o.DeletedAt.Valid {
				return fmt.Errorf("content_key %q 已被活跃 mid=%d 占用，无法写入 mid=%d（请检查脏数据或重置冲突片）",
					key, o.Mid, item.Mid)
			}
			if err := tx.Unscoped().Model(&model.FilmIndex{}).
				Where("id = ?", o.ID).
				Update("content_key", fmt.Sprintf("del_%d", o.ID)).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// keyToMidFromIndexes 主站 mid 即全局 mid，映射可直接由本批索引构造（不依赖 DB content_key 是否已懒升）。
func keyToMidFromIndexes(list []model.FilmIndex) map[string]int64 {
	out := make(map[string]int64, len(list))
	for _, item := range list {
		key := strings.TrimSpace(item.ContentKey)
		if key == "" || item.Mid <= 0 {
			continue
		}
		out[key] = item.Mid
	}
	return out
}

func loadFilmIndexMidMapByContentKeys(contentKeys []string) map[string]int64 {
	return loadFilmIndexMidMapByContentKeysTx(db.Mdb, contentKeys)
}

func loadFilmIndexMidMapByContentKeysTx(tx *gorm.DB, contentKeys []string) map[string]int64 {
	if len(contentKeys) == 0 {
		return nil
	}

	var latestInfos []model.FilmIndex
	if err := tx.Where("content_key IN ?", contentKeys).Find(&latestInfos).Error; err != nil {
		return nil
	}

	keyToMid := make(map[string]int64, len(latestInfos))
	for _, info := range latestInfos {
		keyToMid[info.ContentKey] = info.Mid
	}
	return keyToMid
}

func buildContentKeys(list []model.FilmIndex) []string {
	contentKeys := make([]string, 0, len(list))
	for _, item := range list {
		contentKeys = append(contentKeys, item.ContentKey)
	}
	return contentKeys
}

func buildMovieSourceMappings(list []model.FilmIndex, keyToMid map[string]int64) []model.MovieSourceMapping {
	mappings := make([]model.MovieSourceMapping, 0, len(list))
	for _, item := range list {
		globalMid, ok := keyToMid[item.ContentKey]
		if !ok {
			continue
		}
		mappings = append(mappings, model.MovieSourceMapping{
			SourceId:  item.SourceId,
			SourceMid: item.Mid,
			GlobalMid: globalMid,
		})
	}
	return mappings
}

func saveFilmIndexesAndMappings(list []model.FilmIndex) (map[string]int64, error) {
	return saveFilmIndexesAndMappingsTx(db.Mdb, list)
}

func saveFilmIndexesAndMappingsTx(tx *gorm.DB, list []model.FilmIndex) (map[string]int64, error) {
	if len(list) == 0 {
		return nil, nil
	}

	if err := upsertFilmIndexesTx(tx, list); err != nil {
		return nil, err
	}

	keyToMid := keyToMidFromIndexes(list)
	if len(keyToMid) == 0 {
		return nil, fmt.Errorf("load film index mids failed")
	}
	if err := saveMovieSourceMappingsTxE(tx, buildMovieSourceMappings(list, keyToMid)); err != nil {
		return nil, err
	}
	return keyToMid, nil
}

func saveMovieSourceMappingsTxE(tx *gorm.DB, mappings []model.MovieSourceMapping) error {
	if len(mappings) == 0 {
		return nil
	}
	movieSourceMappingWriteMu.Lock()
	defer movieSourceMappingWriteMu.Unlock()
	sort.Slice(mappings, func(i, j int) bool {
		if mappings[i].SourceId == mappings[j].SourceId {
			return mappings[i].SourceMid < mappings[j].SourceMid
		}
		return mappings[i].SourceId < mappings[j].SourceId
	})
	return tx.Clauses(movieSourceMappingUpsert()).CreateInBatches(&mappings, upsertBatchSize).Error
}

func reloadFilmIndexesByContentKeys(contentKeys []string) []model.FilmIndex {
	return reloadFilmIndexesByContentKeysTx(db.Mdb, contentKeys)
}

func reloadFilmIndexesByContentKeysTx(tx *gorm.DB, contentKeys []string) []model.FilmIndex {
	if len(contentKeys) == 0 {
		return nil
	}
	var infos []model.FilmIndex
	if err := tx.Where("content_key IN ?", contentKeys).Find(&infos).Error; err != nil {
		return nil
	}
	return infos
}

func filmIndexMIDs(infos []model.FilmIndex) []int64 {
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
	return mids
}

func reloadFilmIndexesByMidsTx(tx *gorm.DB, mids []int64) []model.FilmIndex {
	if len(mids) == 0 {
		return nil
	}
	var infos []model.FilmIndex
	if err := tx.Where("mid IN ?", mids).Find(&infos).Error; err != nil {
		return nil
	}
	return infos
}

func collectFilmIndexMIDs(infos []model.FilmIndex) []int64 {
	midSet := make(map[int64]struct{}, len(infos))
	mids := make([]int64, 0, len(infos))
	for _, info := range infos {
		if info.Mid <= 0 {
			continue
		}
		if _, ok := midSet[info.Mid]; ok {
			continue
		}
		midSet[info.Mid] = struct{}{}
		mids = append(mids, info.Mid)
	}
	return mids
}

func filmIndexContentKeys(infos []model.FilmIndex) []string {
	keys := make([]string, 0, len(infos))
	seen := make(map[string]struct{}, len(infos))
	for _, info := range infos {
		key := strings.TrimSpace(info.ContentKey)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}
