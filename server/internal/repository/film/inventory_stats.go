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
	stats.Library.Playable = countPlayableFilms()

	filmCounts := indexCounts(countFilmsBySource())
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
			Films:      filmCounts[id],
			Categories: categoryCounts[id],
			Failures:   failureCounts[id],
		})
	}
	return stats
}

func countPlayableFilms() int64 {
	var n int64
	_ = db.Mdb.Table(model.TableFilmSourcePlaylist+" AS p").
		Joins("INNER JOIN "+model.TableFilmIndex+" AS f ON f.mid = p.mid AND f.deleted_at IS NULL").
		Where("p.line_kind = ?", "play").
		Distinct("p.mid").
		Count(&n).Error
	return n
}

func countFilmsBySource() []sourceCount {
	var rows []sourceCount
	_ = db.Mdb.Table(model.TableFilmSourcePlaylist+" AS p").
		Select("p.source_id AS source_id, COUNT(DISTINCT p.mid) AS n").
		Joins("INNER JOIN "+model.TableFilmIndex+" AS f ON f.mid = p.mid AND f.deleted_at IS NULL").
		Where("p.line_kind = ?", "play").
		Group("p.source_id").
		Scan(&rows).Error
	return rows
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
