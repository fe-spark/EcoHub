package shared

import (
	"sort"
	"strings"
	"sync"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UpsertBatchSize 批量 upsert 的分批大小。
const UpsertBatchSize = 200

// CollectWriteResult 采集写入结果。
// AffectedMIDs：有业务写入的 mid（缓存/快照收尾）；NotifyMIDs：应进更新列表的 mid（剧集结构变更或新片）。
type CollectWriteResult struct {
	AffectedMIDs []int64
	NotifyMIDs   []int64
}

var movieSourceMappingWriteMu sync.Mutex

func movieSourceMappingUpsert() clause.OnConflict {
	return clause.OnConflict{
		Columns:   []clause.Column{{Name: "source_id"}, {Name: "source_mid"}},
		DoUpdates: clause.AssignmentColumns([]string{"global_mid", "updated_at", "deleted_at"}),
	}
}

// BuildMovieSourceMappings 由 content_key→mid 映射生成主从站来源映射行。
func BuildMovieSourceMappings(list []model.FilmIndex, keyToMid map[string]int64) []model.MovieSourceMapping {
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

// SaveMovieSourceMappingsTxE 按 (source_id, source_mid) 冲突 upsert 来源映射。
func SaveMovieSourceMappingsTxE(tx *gorm.DB, mappings []model.MovieSourceMapping) error {
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
	return tx.Clauses(movieSourceMappingUpsert()).CreateInBatches(&mappings, UpsertBatchSize).Error
}

// LoadGlobalMidsBySourceMids 按源站 vod_id 反查已匹配的本地 mid。
func LoadGlobalMidsBySourceMids(sourceID string, sourceMids []int64) map[int64]int64 {
	out := make(map[int64]int64, len(sourceMids))
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || len(sourceMids) == 0 || db.Mdb == nil {
		return out
	}
	uniq := make([]int64, 0, len(sourceMids))
	seen := make(map[int64]struct{}, len(sourceMids))
	for _, id := range sourceMids {
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
		return out
	}
	var rows []model.MovieSourceMapping
	if err := db.Mdb.Select("source_mid", "global_mid").
		Where("source_id = ? AND source_mid IN ?", sourceID, uniq).
		Find(&rows).Error; err != nil {
		return out
	}
	for _, row := range rows {
		if row.SourceMid > 0 && row.GlobalMid > 0 {
			out[row.SourceMid] = row.GlobalMid
		}
	}
	return out
}
