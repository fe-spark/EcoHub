package film

import (
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
)

// LibraryInventory 工作台片库规模，只含当前主站。
// 分类是该站已映射且展示分类还在的节点。失败数是该站北京时间当天的采集失败页。
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

// InventoryStats 工作台片库规模。首屏只填当前主站，展开时再填各站。
type InventoryStats struct {
	Library LibraryInventory  `json:"library"`
	Sources []SourceInventory `json:"sources"`
}

// GetLibraryInventory 统计当前主站的档案、分类和当天采集失败页。
func GetLibraryInventory() (LibraryInventory, error) {
	var library LibraryInventory
	if db.Mdb == nil {
		return library, filmsnapshot.ErrSearchIndexMissing
	}
	primarySource := repository.CurrentPrimarySource()
	if primarySource == nil || strings.TrimSpace(primarySource.Id) == "" {
		return library, nil
	}
	primary := strings.TrimSpace(primarySource.Id)
	categories, err := countPrimaryCategories(primary)
	if err != nil {
		return library, err
	}
	// CurrentPrimarySource 走内存缓存，采集增减或重算 film_count 不会刷新该缓存，这里回查数据库以与各站统计保持一致。
	if latest := repository.FindCollectSourceById(primary); latest != nil {
		library.Films = latest.FilmCount
	}
	library.Categories = categories
	if _, bySource, err := repository.CountTodayCollectFailures(time.Now()); err == nil {
		library.Failures = bySource[primary]
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
