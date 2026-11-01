package service

import (
	"errors"
	"fmt"
	"strings"

	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/repository/film/writer"
	"server/internal/repository/support"
)

// ApplyDetail 把刮削结果写入影片。写入后就是这部影片的内容。
func (s *TMDBService) ApplyDetail(req model.TMDBApplyReq) error {
	_, err := s.ApplyDetailWithOptions(req, true)
	return err
}

// ApplyDetailWithOptions 把选中的刮削字段写入 film_index。片名和分类不改。词条只记 mid 与 tmdb_id。
func (s *TMDBService) ApplyDetailWithOptions(req model.TMDBApplyReq, publishSnapshot bool) (int64, error) {
	if req.Mid <= 0 || req.TmdbID <= 0 {
		return 0, errors.New("影片ID或 TMDB ID 参数非法")
	}

	tmdbData, err := s.FetchDetail(req.TmdbID, req.MediaType)
	if err != nil {
		return 0, fmt.Errorf("获取 TMDB 详情失败: %w", err)
	}
	if tmdbData == nil {
		return 0, errors.New("获取 TMDB 详情失败")
	}

	indexRec, err := filmrepo.FindFilmIndex(req.Mid)
	if err != nil {
		return 0, fmt.Errorf("未找到对应的影片信息 (mid=%d): %w", req.Mid, err)
	}

	updates := filmScrapeUpdates(indexRec, *tmdbData, req.Fields)
	scrapeRecord := model.FilmTMDBScrape{
		Mid:       req.Mid,
		TmdbID:    req.TmdbID,
		MediaType: strings.TrimSpace(req.MediaType),
	}
	if err := repository.SaveFilmScrape(req.Mid, updates, scrapeRecord); err != nil {
		return 0, fmt.Errorf("写入影片失败: %w", err)
	}

	if publishSnapshot {
		if _, _, err := filmsnapshot.UpsertActiveSnapshotsByMids(req.Mid); err != nil {
			syslog.Errorf("[TMDB] 影片 %d 快照发布失败: %v", req.Mid, err)
			return req.Mid, err
		}
		filmsnapshot.ClearDynamicPlayCaches()
		support.ClearIndexPageCache()
	}

	// 清理分类列表缓存
	if indexRec.Pid > 0 {
		writer.ClearFilmIndexCachesByPidSet(map[int64]struct{}{indexRec.Pid: {}})
	}

	return req.Mid, nil
}

// FetchFormPrefill 拉取用于前端表单自动填充的元数据
func (s *TMDBService) FetchFormPrefill(tmdbID int64, mediaType string) (map[string]any, error) {
	d, err := s.FetchDetail(tmdbID, mediaType)
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"name":         d.Title,
		"subTitle":     d.OriginalTitle,
		"picture":      d.Poster,
		"pictureSlide": d.Backdrop,
		"content":      d.Overview,
		"actor":        strings.Join(d.Actors, "/"),
		"director":     strings.Join(d.Directors, "/"),
		"writer":       strings.Join(d.Writers, "/"),
		"area":         d.Area,
		"language":     d.Language,
		"year":         d.Year,
		"releaseDate":  d.ReleaseDate,
		"dbScore":      d.VoteScore,
		"classTag":     strings.Join(d.Genres, ","),
	}
	return result, nil
}
