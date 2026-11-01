package service

import (
	"errors"
	"strconv"
	"strings"

	"server/internal/model"
	filmrepo "server/internal/repository/film"
)

// UseStoredFilm 编辑页使用影片自己保存的正文和封面，不用前台海报源替换结果。
func UseStoredFilm(detail *model.MovieDetailVo) error {
	if detail == nil || detail.Id <= 0 {
		return errors.New("影片信息无效")
	}
	row, err := filmrepo.FindFilmIndex(detail.Id)
	if err != nil {
		return err
	}
	if detail.SourceId == "" && row.FirstSourceId != "" {
		detail.SourceId = row.FirstSourceId
	}
	if detail.RawCid == 0 && row.CategoryKey != "" {
		parts := strings.Split(row.CategoryKey, ":")
		if len(parts) >= 3 {
			if id, err := strconv.ParseInt(parts[len(parts)-1], 10, 64); err == nil && id > 0 {
				detail.RawCid = id
			}
		}
	}
	if detail.RawPid == 0 && row.RootCategoryKey != "" {
		parts := strings.Split(row.RootCategoryKey, ":")
		if len(parts) >= 3 {
			if id, err := strconv.ParseInt(parts[len(parts)-1], 10, 64); err == nil && id > 0 {
				detail.RawPid = id
			}
		}
	}
	if row.CName != "" {
		detail.CName = row.CName
	}
	detail.Picture = row.Picture
	detail.PictureSlide = row.PictureSlide
	detail.CustomPicture = row.CustomPicture
	detail.CustomPictureSlide = row.CustomPictureSlide
	detail.IsCustomPicture = row.IsCustomPicture
	detail.SubTitle = row.SubTitle
	detail.ClassTag = row.ClassTag
	detail.Actor = row.Actor
	detail.Director = row.Director
	detail.Writer = row.Writer
	detail.Blurb = row.Blurb
	detail.Content = row.Content
	detail.ReleaseDate = row.ReleaseDate
	detail.Area = row.Area
	detail.Language = row.Language
	if row.Year > 0 {
		detail.Year = strconv.FormatInt(row.Year, 10)
	}
	if row.Score > 0 {
		detail.DbScore = strconv.FormatFloat(row.Score, 'f', -1, 64)
	}
	if row.DbId > 0 {
		detail.DbId = row.DbId
	}
	if row.Hits > 0 {
		detail.Hits = row.Hits
	}
	if row.Initial != "" {
		detail.Initial = row.Initial
	}
	if row.State != "" {
		detail.State = row.State
	}
	if strings.TrimSpace(row.Remarks) != "" {
		detail.Remarks = row.Remarks
	}
	return nil
}
