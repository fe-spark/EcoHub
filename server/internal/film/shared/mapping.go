package shared

import (
	"fmt"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UpsertBatchSize 批量 upsert 的分批大小。
const UpsertBatchSize = 200

func movieSourceMappingUpsert() clause.OnConflict {
	return clause.OnConflict{
		Columns:   []clause.Column{{Name: "source_id"}, {Name: "source_mid"}},
		DoUpdates: clause.AssignmentColumns([]string{"global_mid", "updated_at"}),
	}
}

// SaveMovieSourceMappingTx 保存单条源站影片 ID 与全局 Mid 映射
func SaveMovieSourceMappingTx(tx *gorm.DB, sourceID string, sourceMid, globalMid int64) error {
	mapping := model.MovieSourceMapping{
		SourceId:  sourceID,
		SourceMid: sourceMid,
		GlobalMid: globalMid,
	}
	return tx.Clauses(movieSourceMappingUpsert()).Create(&mapping).Error
}

// LoadSourceMidsByGlobalMids 按本地 mid 反查当前来源的源站 vod_id。
func LoadSourceMidsByGlobalMids(sourceID string, globalMids []int64) (map[int64]int64, error) {
	out := make(map[int64]int64, len(globalMids))
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || len(globalMids) == 0 {
		return out, nil
	}
	if db.Mdb == nil {
		return nil, fmt.Errorf("数据库未就绪")
	}
	uniq := make([]int64, 0, len(globalMids))
	seen := make(map[int64]struct{}, len(globalMids))
	for _, id := range globalMids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return out, nil
	}
	var rows []model.MovieSourceMapping
	if err := db.Mdb.Select("source_mid", "global_mid").
		Where("source_id = ? AND global_mid IN ?", sourceID, uniq).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.SourceMid > 0 && row.GlobalMid > 0 {
			out[row.GlobalMid] = row.SourceMid
		}
	}
	return out, nil
}
