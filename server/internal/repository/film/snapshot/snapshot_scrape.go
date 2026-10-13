package snapshot

import (
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
)

// LoadActiveScrapesByMids 批量加载已启用的 TMDB 刮削元数据
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
		if err := db.Mdb.Where("mid IN ? AND status = 1", cleanMids[i:end]).Find(&rows).Error; err != nil {
			continue
		}
		for _, r := range rows {
			out[r.Mid] = r
		}
	}
	return out
}

// ParseAppliedFields 解析应用的字段列表集合
func ParseAppliedFields(raw string) map[string]struct{} {
	set := make(map[string]struct{})
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return set
	}
	parts := strings.Split(raw, ",")
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			set[p] = struct{}{}
		}
	}
	return set
}

// ShouldApplyField 判断指定字段是否处于应用集合中（若集合为空则默认全量应用）
func ShouldApplyField(fieldSet map[string]struct{}, names ...string) bool {
	if len(fieldSet) == 0 {
		return true
	}
	for _, name := range names {
		if _, ok := fieldSet[strings.ToLower(name)]; ok {
			return true
		}
	}
	return false
}

// ApplyTMDBScrapeToSnapshot 将 TMDB 刮削元数据合成投影到 FilmListSnapshot 读模型
func ApplyTMDBScrapeToSnapshot(snap *model.FilmListSnapshot, sc *model.FilmTMDBScrape) {
	if snap == nil || sc == nil || sc.Status != 1 {
		return
	}
	snap.IsScraped = true
	snap.TmdbID = sc.TmdbID

	fieldSet := ParseAppliedFields(sc.AppliedFields)

	// 1. 竖版海报（锁定保护）
	if ShouldApplyField(fieldSet, "poster", "picture") && sc.Poster != "" {
		snap.Picture = sc.Poster
		snap.CustomPicture = sc.Poster
		snap.IsCustomPicture = true
	}
	// 2. 横版幻灯大图
	if ShouldApplyField(fieldSet, "backdrop", "pictureslide") && sc.Backdrop != "" {
		snap.PictureSlide = sc.Backdrop
		snap.CustomPictureSlide = sc.Backdrop
	}
	// 3. 剧情简介
	if ShouldApplyField(fieldSet, "overview", "content") && sc.Overview != "" {
		snap.Content = sc.Overview
		snap.Blurb = sc.Overview
	}
	// 4. 副标题/原名
	if ShouldApplyField(fieldSet, "subtitle", "originaltitle") && sc.OriginalTitle != "" {
		snap.SubTitle = sc.OriginalTitle
	}
	// 5. 演员
	if ShouldApplyField(fieldSet, "actor", "credits") && sc.Actors != "" {
		snap.Actor = sc.Actors
	}
	// 6. 导演
	if ShouldApplyField(fieldSet, "director", "credits") && sc.Directors != "" {
		snap.Director = sc.Directors
	}
	// 7. 年份与上映日期
	if ShouldApplyField(fieldSet, "year", "releasedate") {
		if sc.Year > 0 {
			snap.Year = sc.Year
		}
		if sc.ReleaseDate != "" {
			snap.ReleaseDate = sc.ReleaseDate
		}
	}
	// 8. 评分
	if ShouldApplyField(fieldSet, "score", "voteaverage") && sc.VoteAverage > 0 {
		snap.Score = sc.VoteAverage
	}
	// 9. 分类标签
	if ShouldApplyField(fieldSet, "tag", "genres") && sc.Genres != "" {
		snap.ClassTag = sc.Genres
	}
}

// ApplyTMDBScrapeToFilmIndex 将 TMDB 刮削元数据合成投影到 FilmIndex 展示层
func ApplyTMDBScrapeToFilmIndex(idx *model.FilmIndex, sc *model.FilmTMDBScrape) {
	if idx == nil || sc == nil || sc.Status != 1 {
		return
	}
	idx.IsScraped = true
	idx.TmdbID = sc.TmdbID

	fieldSet := ParseAppliedFields(sc.AppliedFields)

	if ShouldApplyField(fieldSet, "poster", "picture") && sc.Poster != "" {
		idx.Picture = sc.Poster
		idx.CustomPicture = sc.Poster
		idx.IsCustomPicture = true
	}
	if ShouldApplyField(fieldSet, "backdrop", "pictureslide") && sc.Backdrop != "" {
		idx.PictureSlide = sc.Backdrop
		idx.CustomPictureSlide = sc.Backdrop
	}
	if ShouldApplyField(fieldSet, "overview", "content") && sc.Overview != "" {
		idx.Content = sc.Overview
		idx.Blurb = sc.Overview
	}
	if ShouldApplyField(fieldSet, "subtitle", "originaltitle") && sc.OriginalTitle != "" {
		idx.SubTitle = sc.OriginalTitle
	}
	if ShouldApplyField(fieldSet, "actor", "credits") && sc.Actors != "" {
		idx.Actor = sc.Actors
	}
	if ShouldApplyField(fieldSet, "director", "credits") && sc.Directors != "" {
		idx.Director = sc.Directors
	}
	if ShouldApplyField(fieldSet, "year", "releasedate") {
		if sc.Year > 0 {
			idx.Year = sc.Year
		}
		if sc.ReleaseDate != "" {
			idx.ReleaseDate = sc.ReleaseDate
		}
	}
	if ShouldApplyField(fieldSet, "score", "voteaverage") && sc.VoteAverage > 0 {
		idx.Score = sc.VoteAverage
	}
	if ShouldApplyField(fieldSet, "tag", "genres") && sc.Genres != "" {
		idx.ClassTag = sc.Genres
	}
}
