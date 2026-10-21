package film

import (
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
)

// LibraryInventory 工作台片库规模。影片和多源聚合按当前主站。分类和失败记录是全库。
type LibraryInventory struct {
	Films       int64 `json:"films"`
	MultiSource int64 `json:"multiSource"`
	Categories  int64 `json:"categories"`
	Failures    int64 `json:"failures"`
}

// SourceInventory 单个采集站的规模。影片、多源聚合、分类、失败记录各自跟当前列表口径。
type SourceInventory struct {
	Id          string `json:"id"`
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	IsPrimary   bool   `json:"isPrimary"`
	Films       int64  `json:"films"`
	MultiSource int64  `json:"multiSource"`
	Categories  int64  `json:"categories"`
	Failures    int64  `json:"failures"`
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

// GetInventoryStats 统计整库和各采集站。工作台首屏只取整库，各站在展开时另取。
func GetInventoryStats() (InventoryStats, error) {
	library, err := GetLibraryInventory()
	if err != nil {
		return InventoryStats{}, err
	}
	sources, err := GetSourceInventories()
	if err != nil {
		return InventoryStats{}, err
	}
	return InventoryStats{Library: library, Sources: sources}, nil
}

// GetLibraryInventory 统计当前主站档案、多源聚合、展示分类和失败记录。
func GetLibraryInventory() (LibraryInventory, error) {
	var library LibraryInventory
	if db.Mdb == nil {
		return library, filmsnapshot.ErrSearchIndexMissing
	}
	films, multiSource, err := filmsnapshot.SearchInventoryCounts()
	if err != nil {
		return library, err
	}
	if primaryID := activePrimaryID(); primaryID != "" {
		library.Films = films[primaryID]
		library.MultiSource = multiSource[primaryID]
	}
	if err := db.Mdb.Model(&model.Category{}).Count(&library.Categories).Error; err != nil {
		return library, err
	}
	if err := db.Mdb.Model(&model.FailureRecord{}).Count(&library.Failures).Error; err != nil {
		return library, err
	}
	return library, nil
}

func activePrimaryID() string {
	if primary := repository.GetActiveCollectSource(); primary != nil {
		return strings.TrimSpace(primary.Id)
	}
	return ""
}

// GetSourceInventories 按采集站统计影片、多源聚合、分类和失败记录。
// 影片是该站自己的档案，含没有线路的。分类只计该站映射到、且展示分类还在的节点。
func GetSourceInventories() ([]SourceInventory, error) {
	sources := []SourceInventory{}
	if db.Mdb == nil {
		return nil, filmsnapshot.ErrSearchIndexMissing
	}
	films, multiSource, err := filmsnapshot.SearchInventoryCounts()
	if err != nil {
		return nil, err
	}
	categoryRows, err := countCategoriesBySource()
	if err != nil {
		return nil, err
	}
	failureRows, err := countFailuresBySource()
	if err != nil {
		return nil, err
	}
	categoryCounts := indexCounts(categoryRows)
	failureCounts := indexCounts(failureRows)
	primaryID := activePrimaryID()
	sourceList, err := repository.GetCollectSourceList()
	if err != nil {
		return nil, err
	}
	for _, source := range sourceList {
		id := strings.TrimSpace(source.Id)
		if id == "" {
			continue
		}
		sources = append(sources, SourceInventory{
			Id:          id,
			Name:        source.Name,
			Enabled:     source.State,
			IsPrimary:   id == primaryID,
			Films:       films[id],
			MultiSource: multiSource[id],
			Categories:  categoryCounts[id],
			Failures:    failureCounts[id],
		})
	}
	return sources, nil
}

func countCategoriesBySource() ([]sourceCount, error) {
	var rows []sourceCount
	err := db.Mdb.Model(&model.CategoryMapping{}).
		Select("source_id, COUNT(DISTINCT category_id) AS n").
		Where("category_id > 0").
		Where("category_id IN (?)", db.Mdb.Model(&model.Category{}).Select("id")).
		Group("source_id").
		Scan(&rows).Error
	return rows, err
}

func countFailuresBySource() ([]sourceCount, error) {
	var rows []sourceCount
	err := db.Mdb.Model(&model.FailureRecord{}).
		Select("origin_id AS source_id, COUNT(*) AS n").
		Group("origin_id").
		Scan(&rows).Error
	return rows, err
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
