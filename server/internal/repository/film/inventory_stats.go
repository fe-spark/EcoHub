package film

import (
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
)

// LibraryInventory 工作台片库规模，只含当前主站。
// 失败数是该站北京时间当天的采集失败页。失败请求没有影片列表，不能按部数统计。
type LibraryInventory struct {
	Films       int64 `json:"films"`
	MultiSource int64 `json:"multiSource"`
	Failures    int64 `json:"failures"`
}

// GetLibraryInventory 统计当前主站的档案、多源聚合，以及当天采集失败页。
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
	library.Films = films
	if films > 0 {
		multi, err := countPrimaryMulti(primary)
		if err != nil {
			return library, err
		}
		library.MultiSource = multi
	}
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
