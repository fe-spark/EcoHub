package film

import (
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
)

// LibraryInventory 工作台片库规模，汇总全部采集站。
// 影片、分类与当天失败页均为下方各采集站对应指标之和。
type LibraryInventory struct {
	Films      int64 `json:"films"`
	Categories int64 `json:"categories"`
	Failures   int64 `json:"failures"`
}

// SourceInventory 单个采集站的规模。影片、分类和失败页与顶部同一口径。
type SourceInventory struct {
	Id         string `json:"id"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	IsPrimary  bool   `json:"isPrimary"`
	Films      int64  `json:"films"`
	Categories int64  `json:"categories"`
	Failures   int64  `json:"failures"`
}

// InventoryStats 工作台片库规模。首屏只填汇总指标，展开时再填各站。
type InventoryStats struct {
	Library LibraryInventory  `json:"library"`
	Sources []SourceInventory `json:"sources"`
}

// GetLibraryInventory 汇总全部采集站的档案总数、分类总数和当天采集失败页总数。
func GetLibraryInventory() (LibraryInventory, error) {
	var library LibraryInventory
	sources, err := GetSourceInventories()
	if err != nil {
		return library, err
	}
	for _, source := range sources {
		library.Films += source.Films
		library.Categories += source.Categories
		library.Failures += source.Failures
	}
	return library, nil
}

// GetSourceInventories 按采集站读取档案数、分类和当天失败页。档案数在采集站上，不扫影片表。
func GetSourceInventories() ([]SourceInventory, error) {
	if db.Mdb == nil {
		return nil, filmsnapshot.ErrSearchIndexMissing
	}
	categories, err := countCategoriesBySource()
	if err != nil {
		return nil, err
	}
	_, failureCounts, _ := repository.CountTodayCollectFailures(time.Now())
	primaryID := activePrimaryID()
	sourceList, err := repository.GetCollectSourceList()
	if err != nil {
		return nil, err
	}
	sources := make([]SourceInventory, 0, len(sourceList))
	for _, source := range sourceList {
		id := strings.TrimSpace(source.Id)
		if id == "" {
			continue
		}
		sources = append(sources, SourceInventory{
			Id:         id,
			Name:       source.Name,
			Enabled:    source.State,
			IsPrimary:  id == primaryID,
			Films:      source.FilmCount,
			Categories: categories[id],
			Failures:   failureCounts[id],
		})
	}
	return sources, nil
}

func activePrimaryID() string {
	if primary := repository.CurrentPrimarySource(); primary != nil {
		return strings.TrimSpace(primary.Id)
	}
	return ""
}
