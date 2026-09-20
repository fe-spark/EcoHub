package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/film/writer"
)

// ApplyDetail 将 TMDB 刮削出的元数据应用到指定影片并落库刷新
func (s *TMDBService) ApplyDetail(req model.TMDBApplyReq) error {
	if req.Mid <= 0 || req.TmdbID <= 0 {
		return errors.New("影片ID或 TMDB ID 参数非法")
	}

	tmdbData, err := s.FetchDetail(req.TmdbID, req.MediaType)
	if err != nil {
		return fmt.Errorf("获取 TMDB 详情失败: %w", err)
	}

	var detailRec model.MovieDetailInfo
	if err := db.Mdb.Where("mid = ?", req.Mid).First(&detailRec).Error; err != nil {
		return fmt.Errorf("未找到对应的影片信息 (mid=%d): %w", req.Mid, err)
	}

	var detail model.MovieDetail
	if err := json.Unmarshal([]byte(detailRec.Content), &detail); err != nil {
		return fmt.Errorf("解析既有影片数据失败: %w", err)
	}
	detail.Id = req.Mid

	var indexRec model.FilmIndex
	_ = db.Mdb.Where("mid = ?", req.Mid).First(&indexRec).Error

	fieldSet := make(map[string]struct{}, len(req.Fields))
	for _, f := range req.Fields {
		fieldSet[strings.ToLower(strings.TrimSpace(f))] = struct{}{}
	}
	applyAll := len(fieldSet) == 0

	shouldApply := func(names ...string) bool {
		if applyAll {
			return true
		}
		for _, name := range names {
			if _, ok := fieldSet[strings.ToLower(name)]; ok {
				return true
			}
		}
		return false
	}

	// 字段覆盖
	if shouldApply("poster", "picture") && tmdbData.Poster != "" {
		detail.Picture = tmdbData.Poster
		detail.CustomPicture = tmdbData.Poster
		detail.IsCustomPicture = true
	}
	// 仅写横图，不设置 IsCustomPicture，避免排片刮削把片库封面锁死
	if shouldApply("backdrop", "pictureslide") && tmdbData.Backdrop != "" {
		detail.PictureSlide = tmdbData.Backdrop
		detail.CustomPictureSlide = tmdbData.Backdrop
	}
	if shouldApply("overview", "content") && tmdbData.Overview != "" {
		detail.MovieDescriptor.Content = tmdbData.Overview
		detail.MovieDescriptor.Blurb = tmdbData.Overview
	}
	if shouldApply("subtitle", "originaltitle") && tmdbData.OriginalTitle != "" {
		detail.MovieDescriptor.SubTitle = tmdbData.OriginalTitle
	}
	if shouldApply("actor", "credits") && len(tmdbData.Actors) > 0 {
		detail.MovieDescriptor.Actor = strings.Join(tmdbData.Actors, "/")
	}
	if shouldApply("director", "credits") && len(tmdbData.Directors) > 0 {
		detail.MovieDescriptor.Director = strings.Join(tmdbData.Directors, "/")
	}
	if shouldApply("year", "releasedate") {
		if tmdbData.Year != "" {
			detail.MovieDescriptor.Year = tmdbData.Year
		}
		if tmdbData.ReleaseDate != "" {
			detail.MovieDescriptor.ReleaseDate = tmdbData.ReleaseDate
		} else if detail.MovieDescriptor.ReleaseDate == "" && tmdbData.Year != "" {
			detail.MovieDescriptor.ReleaseDate = tmdbData.Year
		}
	}
	if shouldApply("score", "voteaverage") && tmdbData.VoteScore != "" {
		detail.MovieDescriptor.DbScore = tmdbData.VoteScore
	}
	if shouldApply("tag", "genres") && len(tmdbData.Genres) > 0 {
		detail.MovieDescriptor.ClassTag = strings.Join(tmdbData.Genres, ",")
	}

	detail.MovieDescriptor.UpdateTime = time.Now().Format(time.DateTime)

	sourceID := indexRec.SourceId
	if sourceID == "" {
		sourceID = detailRec.SourceId
	}
	if sourceID == "" {
		sourceID = "manual"
	}

	return writer.SaveDetail(sourceID, detail)
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
		"year":         d.Year,
		"releaseDate":  d.ReleaseDate,
		"dbScore":      d.VoteScore,
		"classTag":     strings.Join(d.Genres, ","),
	}
	return result, nil
}
