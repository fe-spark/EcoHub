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

// GetLibraryInventory 统计当前主站的档案、分类和当天采集失败页。
func GetLibraryInventory() (LibraryInventory, error) {
	var library LibraryInventory
	if db.Mdb == nil {
		return library, filmsnapshot.ErrSearchIndexMissing
	}
	primary := activePrimaryID()
	if primary == "" {
		return library, nil
	}
	films, err := countFilms(primary)
	if err != nil {
		return library, err
	}
	categories, err := countPrimaryCategories(primary)
	if err != nil {
		return library, err
	}
	library.Films = films
	library.Categories = categories
	if _, bySource, err := repository.CountTodayCollectFailures(time.Now()); err == nil {
		library.Failures = bySource[primary]
	}
	return library, nil
}

func activePrimaryID() string {
	if primary := repository.GetActiveCollectSource(); primary != nil {
		return strings.TrimSpace(primary.Id)
	}
	return ""
}
