package snapshot

import (
	"server/internal/infra/db"
	"server/internal/model"
)

// LoadActiveScrapesByMids 批量加载影片对应的 TMDB 词条。
func LoadActiveScrapesByMids(mids []int64) map[int64]model.FilmTMDBScrape {
	out := make(map[int64]model.FilmTMDBScrape)
	if len(mids) == 0 || db.Mdb == nil {
		return out
	}
	cleanMids := make([]int64, 0, len(mids))
	seen := make(map[int64]struct{}, len(mids))
	for _, id := range mids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		cleanMids = append(cleanMids, id)
	}
	if len(cleanMids) == 0 {
		return out
	}

	const batchSize = 500
	for i := 0; i < len(cleanMids); i += batchSize {
		end := i + batchSize
		if end > len(cleanMids) {
			end = len(cleanMids)
		}
		var rows []model.FilmTMDBScrape
		if err := db.Mdb.Where("mid IN ?", cleanMids[i:end]).Find(&rows).Error; err != nil {
			continue
		}
		for _, r := range rows {
			out[r.Mid] = r
		}
	}
	return out
}

// ApplyTMDBScrapeToSnapshot 只标记词条。正文已经写在 film_index。
func ApplyTMDBScrapeToSnapshot(snap *model.FilmListSnapshot, sc *model.FilmTMDBScrape) {
	if snap == nil || sc == nil || sc.TmdbID <= 0 {
		return
	}
	snap.IsScraped = true
	snap.TmdbID = sc.TmdbID
}

// ApplyTMDBScrapeToFilmIndex 只标记词条。正文已经写在 film_index。
func ApplyTMDBScrapeToFilmIndex(idx *model.FilmIndex, sc *model.FilmTMDBScrape) {
	if idx == nil || sc == nil || sc.TmdbID <= 0 {
		return
	}
	idx.IsScraped = true
	idx.TmdbID = sc.TmdbID
}
