package writer

import (
	"fmt"
	"sort"
	"strings"

	"server/internal/model"
	"server/internal/repository/film/shared"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// filmIndexUpsertUpdateColumns 主站按 mid 冲突更新时写入的列。
// 含 content_key：旧库 name_* 在被再次采集时懒升为 vod_*，无需启动 bulk 迁移。
var filmIndexUpsertUpdateColumns = []string{
	"content_key", "source_id", "cid", "pid", "root_category_key", "category_key", "original_category", "name", "sub_title", "c_name", "class_tag",
	"series_key", "area", "language", "year", "initial", "score",
	"update_stamp", "hits", "state", "remarks", "play_from_summary", "db_id", "collect_stamp", "category_version", "rule_version",
	"picture", "picture_slide", "custom_picture", "custom_picture_slide", "is_custom_picture", "actor", "director", "blurb", "updated_at", "deleted_at",
}

// filmIndexMidUpsert 按 mid 冲突更新（主站 mid = 源站 vod_id）。
// 旧库存 content_key=name_* 时仍能命中同一行，并在更新时懒升 content_key→vod_*。
func filmIndexMidUpsert() clause.OnConflict {
	return clause.OnConflict{
		Columns:   []clause.Column{{Name: "mid"}},
		DoUpdates: clause.AssignmentColumns(filmIndexUpsertUpdateColumns),
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
	return tx.Clauses(filmIndexMidUpsert()).CreateInBatches(&list, shared.UpsertBatchSize).Error
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
