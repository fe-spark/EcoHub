package film

import (
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
)

// LibraryInventory 整库规模。数据重置清空的是这一组，不是单个采集站。
type LibraryInventory struct {
	Films      int64 `json:"films"`
	Playable   int64 `json:"playable"`
	Categories int64 `json:"categories"`
	Failures   int64 `json:"failures"`
}

// SourceInventory 单个采集站的规模。影片、分类、失败记录各自跟当前列表口径。
type SourceInventory struct {
	Id         string `json:"id"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	IsPrimary  bool   `json:"isPrimary"`
	Films      int64  `json:"films"`
	Categories int64  `json:"categories"`
	Failures   int64  `json:"failures"`
}

// InventoryStats 工作台片库规模。
type InventoryStats struct {
	Library LibraryInventory  `json:"library"`
	Sources []SourceInventory `json:"sources"`
}

type sourceCount struct {
	SourceId string `gorm:"column:source_id"`
	N        int64  `gorm:"column:n"`
}

// GetInventoryStats 统计整库和各采集站的影片、分类、失败记录。
// 影片只计仍在片库里、且有该站播放线路的片子。分类只计该站映射到、且展示分类还在的节点。
func GetInventoryStats() InventoryStats {
	stats := InventoryStats{Sources: []SourceInventory{}}
	if db.Mdb == nil {
		return stats
	}

	_ = db.Mdb.Model(&model.FilmIndex{}).Count(&stats.Library.Films).Error
	_ = db.Mdb.Model(&model.Category{}).Count(&stats.Library.Categories).Error
	_ = db.Mdb.Model(&model.FailureRecord{}).Count(&stats.Library.Failures).Error
	// 没有软删除影片时，播放线路上的片子都还在片库里，不必再连表。
	// 连表会让优化器从全表出发，线路一百多万行时要十几秒。
	accurate := hasSoftDeletedFilms()
	stats.Library.Playable = countPlayMids("", accurate)

	categoryCounts := indexCounts(countCategoriesBySource())
	failureCounts := indexCounts(countFailuresBySource())

	primaryID := ""
	if primary := repository.GetActiveCollectSource(); primary != nil {
		primaryID = strings.TrimSpace(primary.Id)
	}
	for _, source := range repository.GetCollectSourceList() {
		id := strings.TrimSpace(source.Id)
		if id == "" {
			continue
		}
		stats.Sources = append(stats.Sources, SourceInventory{
			Id:         id,
			Name:       source.Name,
			Enabled:    source.State,
			IsPrimary:  id == primaryID,
			Films:      countPlayMids(id, accurate),
			Categories: categoryCounts[id],
			Failures:   failureCounts[id],
		})
	}
	return stats
}

func hasSoftDeletedFilms() bool {
	var n int64
	err := db.Mdb.Unscoped().Model(&model.FilmIndex{}).Where("deleted_at IS NOT NULL").Count(&n).Error
	return err != nil || n > 0
}

// countPlayMids 统计有播放线路的影片。sourceID 为空时统计整库。
// accurate 为真时只保留未删除、且仍在片库中的影片。
func countPlayMids(sourceID string, accurate bool) int64 {
	query := db.Mdb.Table(model.TableFilmSourcePlaylist+" AS p").Where("p.line_kind = ?", "play")
	if sourceID != "" {
		query = query.Where("p.source_id = ?", sourceID)
	}
	if accurate {
		query = query.Joins("INNER JOIN " + model.TableFilmIndex + " AS f ON f.mid = p.mid AND f.deleted_at IS NULL")
	}
	var n int64
	_ = query.Distinct("p.mid").Count(&n).Error
	return n
}

func countCategoriesBySource() []sourceCount {
	var rows []sourceCount
	_ = db.Mdb.Model(&model.CategoryMapping{}).
		Select("source_id, COUNT(DISTINCT category_id) AS n").
		Where("category_id > 0").
		Where("category_id IN (?)", db.Mdb.Model(&model.Category{}).Select("id")).
		Group("source_id").
		Scan(&rows).Error
	return rows
}

func countFailuresBySource() []sourceCount {
	var rows []sourceCount
	_ = db.Mdb.Model(&model.FailureRecord{}).
		Select("origin_id AS source_id, COUNT(*) AS n").
		Group("origin_id").
		Scan(&rows).Error
	return rows
}

func indexCounts(rows []sourceCount) map[string]int64 {
	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		id := strings.TrimSpace(row.SourceId)
		if id == "" {
			continue
		}
		out[id] = row.N
	}
	return out
}
